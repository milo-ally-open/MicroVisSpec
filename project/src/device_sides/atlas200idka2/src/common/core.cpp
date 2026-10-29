#include "common/core.hpp"

#include <acl/acl.h>
#include <openssl/evp.h>

#include <algorithm>
#include <array>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <fstream>
#include <iomanip>
#include <regex>
#include <sstream>
#include <stdexcept>
#include <system_error>

namespace yunshao {
namespace {

std::string random_suffix() {
    const auto stamp = std::chrono::steady_clock::now().time_since_epoch().count();
    std::ostringstream result;
    result << std::hex << stamp;
    return result.str();
}

std::string sanitize_component(const std::string& value) {
    static const std::regex valid("^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$");
    if (!std::regex_match(value, valid) || value == "." || value == "..") {
        throw std::invalid_argument("invalid model package identifier");
    }
    return value;
}

}  // namespace

ModelManifest ModelManifest::parse(const Json& value) {
    if (!value.is_object()) throw std::invalid_argument("model manifest must be a JSON object");
    ModelManifest result;
    result.model_id = value.at("model_id").get<std::string>();
    result.version = value.at("version").get<std::string>();
    result.format = value.at("format").get<std::string>();
    result.model_file = value.at("model_file").get<std::string>();
    result.sha256 = value.at("sha256").get<std::string>();
    result.soc_version = value.at("soc_version").get<std::string>();
    result.input_width = value.at("input_width").get<int>();
    result.input_height = value.at("input_height").get<int>();
    result.classes = value.at("classes").get<std::vector<std::string>>();
    validate(result);
    return result;
}

void ModelManifest::validate(const ModelManifest& manifest) {
    (void)sanitize_component(manifest.model_id);
    (void)sanitize_component(manifest.version);
    if (!std::regex_match(manifest.format, std::regex("^[a-z0-9._-]{1,32}$"))) {
        throw std::invalid_argument("model format is invalid");
    }
    const std::filesystem::path model_file(manifest.model_file);
    if (model_file.empty() || model_file.has_parent_path() || model_file.filename() != model_file) {
        throw std::invalid_argument("model_file must be a filename without a path");
    }
    if (!std::regex_match(manifest.sha256, std::regex("^[a-f0-9]{64}$"))) {
        throw std::invalid_argument("manifest SHA-256 is invalid");
    }
    if (!std::regex_match(manifest.soc_version, std::regex("^[A-Za-z0-9._-]{1,64}$"))) {
        throw std::invalid_argument("model SoC version is invalid");
    }
    if (manifest.input_width < 1 || manifest.input_height < 1 ||
        manifest.input_width > 16384 || manifest.input_height > 16384) {
        throw std::invalid_argument("model input dimensions are invalid");
    }
    if (manifest.classes.empty() || manifest.classes.size() > 256) throw std::invalid_argument("class map must contain 1..256 classes");
    std::vector<std::string> seen;
    for (const auto& name : manifest.classes) {
        if (name.empty() || name.size() > 96 || std::find(seen.begin(), seen.end(), name) != seen.end()) {
            throw std::invalid_argument("class map contains an invalid or duplicate class");
        }
        seen.push_back(name);
    }
}

Json ModelManifest::to_json() const {
    return Json{{"model_id", model_id}, {"version", version}, {"format", format}, {"model_file", model_file},
                {"sha256", sha256}, {"soc_version", soc_version}, {"input_width", input_width},
                {"input_height", input_height}, {"classes", classes}};
}

void ModelManifest::validate_against_profile(const ModelManifest& manifest, const ModelProfile& profile) {
    validate(manifest);
    if (manifest.model_id != profile.model_id || manifest.format != profile.format ||
        manifest.model_file != profile.model_file || manifest.soc_version != profile.soc_version ||
        manifest.input_width != profile.input_width || manifest.input_height != profile.input_height ||
        manifest.classes != profile.classes) {
        throw std::invalid_argument("model manifest does not match its configured profile");
    }
}

ModelCatalog::ModelCatalog(std::filesystem::path root, EdgeAppConfig config)
    : root_(std::move(root)), config_(std::move(config)) {
    std::filesystem::create_directories(root_ / "models");
    std::filesystem::create_directories(root_ / "staging");
    std::filesystem::create_directories(root_ / "active");
    std::filesystem::create_directories(root_ / "previous");
}

std::string ModelCatalog::sha256_file(const std::filesystem::path& path) {
    std::ifstream input(path, std::ios::binary);
    if (!input) throw std::runtime_error("cannot read model file");
    EVP_MD_CTX* context = EVP_MD_CTX_new();
    if (context == nullptr) throw std::runtime_error("cannot allocate hash context");
    const auto free_context = [&] { EVP_MD_CTX_free(context); };
    if (EVP_DigestInit_ex(context, EVP_sha256(), nullptr) != 1) {
        free_context();
        throw std::runtime_error("cannot initialize SHA-256");
    }
    std::array<char, 1024 * 1024> buffer{};
    while (input) {
        input.read(buffer.data(), static_cast<std::streamsize>(buffer.size()));
        const auto count = input.gcount();
        if (count > 0 && EVP_DigestUpdate(context, buffer.data(), static_cast<std::size_t>(count)) != 1) {
            free_context();
            throw std::runtime_error("cannot update SHA-256");
        }
    }
    if (!input.eof()) {
        free_context();
        throw std::runtime_error("model read failed");
    }
    std::array<unsigned char, EVP_MAX_MD_SIZE> digest{};
    unsigned int length = 0;
    if (EVP_DigestFinal_ex(context, digest.data(), &length) != 1) {
        free_context();
        throw std::runtime_error("cannot finalize SHA-256");
    }
    free_context();
    std::ostringstream result;
    for (unsigned int i = 0; i < length; ++i) result << std::hex << std::setw(2) << std::setfill('0') << static_cast<int>(digest[i]);
    return result.str();
}

bool ModelCatalog::probe_om(const std::filesystem::path& path, int device_id, std::string* error) {
    bool initialized = false;
    bool device_set = false;
    std::uint32_t model_id = 0;
    bool loaded = false;
    aclError code = aclInit(nullptr);
    if (code != ACL_SUCCESS) {
        if (error) *error = "acl_init_failed";
        return false;
    }
    initialized = true;
    code = aclrtSetDevice(device_id);
    if (code != ACL_SUCCESS) {
        if (error) *error = "npu_unavailable";
    } else {
        device_set = true;
        code = aclmdlLoadFromFile(path.c_str(), &model_id);
        if (code != ACL_SUCCESS) {
            if (error) *error = "model_load_failed";
        } else {
            loaded = true;
        }
    }
    bool ok = loaded;
    if (loaded && aclmdlUnload(model_id) != ACL_SUCCESS) {
        if (error) *error = "model_unload_failed";
        ok = false;
    }
    if (device_set && aclrtResetDevice(device_id) != ACL_SUCCESS) {
        if (error) *error = "npu_reset_failed";
        ok = false;
    }
    if (initialized && aclFinalize() != ACL_SUCCESS) {
        if (error) *error = "acl_finalize_failed";
        ok = false;
    }
    return ok;
}

void ModelCatalog::atomic_symlink(const std::filesystem::path& root, const std::string& link_name,
                                  const std::filesystem::path& target) {
    const auto link = root / link_name;
    std::filesystem::create_directories(link.parent_path());
    const auto temporary = link.parent_path() / (link.filename().string() + ".next." + random_suffix());
    std::error_code ec;
    std::filesystem::create_symlink(target, temporary, ec);
    if (ec) throw std::runtime_error("cannot stage model version link");
    std::filesystem::rename(temporary, link, ec);
    if (ec) {
        std::filesystem::remove(temporary);
        throw std::runtime_error("cannot atomically switch model version link");
    }
}

void ModelCatalog::install(const std::filesystem::path& manifest_path,
                           const std::filesystem::path& model_path, int device_id) {
    std::ifstream manifest_stream(manifest_path);
    if (!manifest_stream) throw std::runtime_error("cannot open model manifest");
    Json value;
    manifest_stream >> value;
    const ModelManifest manifest = ModelManifest::parse(value);
    const auto& profile = config_.require_model(manifest.model_id, false);
    ModelManifest::validate_against_profile(manifest, profile);
    if (!std::filesystem::is_regular_file(model_path) || std::filesystem::file_size(model_path) == 0) {
        throw std::runtime_error("model file is missing or empty");
    }
    if (sha256_file(model_path) != manifest.sha256) throw std::runtime_error("model SHA-256 does not match manifest");

    const auto staging = root_ / "staging" / (manifest.version + "." + random_suffix());
    const auto destination = root_ / "models" / manifest.model_id / manifest.version;
    std::filesystem::create_directories(staging);
    try {
        std::filesystem::copy_file(model_path, staging / manifest.model_file, std::filesystem::copy_options::overwrite_existing);
        std::ofstream(staging / "manifest.json") << manifest.to_json().dump(2) << '\n';
        std::filesystem::permissions(staging / manifest.model_file, std::filesystem::perms::owner_read |
            std::filesystem::perms::group_read | std::filesystem::perms::others_read);
        std::filesystem::permissions(staging / "manifest.json", std::filesystem::perms::owner_read |
            std::filesystem::perms::group_read | std::filesystem::perms::others_read);
        std::string probe_error;
        if (!probe_om(staging / manifest.model_file, device_id, &probe_error)) {
            throw std::runtime_error(probe_error.empty() ? "model_load_failed" : probe_error);
        }
        std::filesystem::create_directories(destination.parent_path());
        if (std::filesystem::exists(destination)) {
            if (sha256_file(destination / manifest.model_file) != manifest.sha256) {
                throw std::runtime_error("model version already exists with different content");
            }
            std::filesystem::remove_all(staging);
        } else {
            std::filesystem::rename(staging, destination);
        }
        const auto active = root_ / "active" / manifest.model_id;
        if (std::filesystem::is_symlink(active)) {
            const auto current = std::filesystem::canonical(active);
            const auto previous_link = root_ / "previous" / manifest.model_id;
            const auto relative_previous = std::filesystem::relative(current, previous_link.parent_path());
            atomic_symlink(root_, "previous/" + manifest.model_id, relative_previous);
        }
        const auto relative_target = std::filesystem::relative(destination, active.parent_path());
        atomic_symlink(root_, "active/" + manifest.model_id, relative_target);
    } catch (...) {
        std::error_code ignored;
        std::filesystem::remove_all(staging, ignored);
        throw;
    }
}

void ModelCatalog::rollback(const std::string& model_id) {
    (void)sanitize_component(model_id);
    const auto previous = root_ / "previous" / model_id;
    if (!std::filesystem::is_symlink(previous)) throw std::runtime_error("previous model version is unavailable");
    const auto target = std::filesystem::canonical(previous);
    const auto& profile = config_.require_model(model_id, false);
    std::ifstream manifest_stream(target / "manifest.json");
    if (!manifest_stream || !std::filesystem::is_regular_file(target / profile.model_file)) {
        throw std::runtime_error("previous model package is incomplete");
    }
    Json manifest_json;
    manifest_stream >> manifest_json;
    const auto manifest = ModelManifest::parse(manifest_json);
    ModelManifest::validate_against_profile(manifest, profile);
    if (sha256_file(target / profile.model_file) != manifest.sha256) {
        throw std::runtime_error("previous model package hash does not match its manifest");
    }
    const auto active = root_ / "active" / model_id;
    const auto active_target = std::filesystem::is_symlink(active) ? std::filesystem::canonical(active) : std::filesystem::path{};
    const auto relative_active = std::filesystem::relative(target, active.parent_path());
    atomic_symlink(root_, "active/" + model_id, relative_active);
    if (!active_target.empty()) {
        const auto previous_relative = std::filesystem::relative(active_target, previous.parent_path());
        atomic_symlink(root_, "previous/" + model_id, previous_relative);
    }
}

Json ModelCatalog::status_one(const std::string& model_id) const {
    const auto active = root_ / "active" / model_id;
    if (!std::filesystem::is_symlink(active)) return Json{{"model_id", model_id}, {"model_state", "model_missing"}};
    try {
        const auto package = std::filesystem::canonical(active);
        std::ifstream manifest_stream(package / "manifest.json");
        if (!manifest_stream) return Json{{"model_state", "model_load_failed"}, {"reason_code", "manifest_missing"}};
        Json value;
        manifest_stream >> value;
        const auto manifest = ModelManifest::parse(value);
        if (manifest.model_id != model_id) return Json{{"model_id", model_id}, {"model_state", "model_load_failed"}, {"reason_code", "model_id_mismatch"}};
        const auto& profile = config_.require_model(model_id, false);
        ModelManifest::validate_against_profile(manifest, profile);
        const auto actual_hash = sha256_file(package / profile.model_file);
        if (actual_hash != manifest.sha256) return Json{{"model_id", model_id}, {"model_state", "model_load_failed"}, {"reason_code", "model_hash_mismatch"}};
        return Json{{"model_state", "installed"}, {"model_id", manifest.model_id}, {"model_version", manifest.version},
                    {"model_format", manifest.format}, {"model_sha256", manifest.sha256},
                    {"soc_version", manifest.soc_version}, {"input_width", manifest.input_width},
                    {"input_height", manifest.input_height}, {"algorithm_key", profile.algorithm_key},
                    {"classes", manifest.classes}};
    } catch (...) {
        return Json{{"model_id", model_id}, {"model_state", "model_load_failed"}, {"reason_code", "invalid_model_package"}};
    }
}

Json ModelCatalog::status(const std::string& model_id) const {
    if (!model_id.empty()) return status_one(model_id);
    Json models = Json::array();
    for (const auto& profile : config_.models) models.push_back(status_one(profile.model_id));
    return Json{{"models", std::move(models)}, {"max_loaded_models", config_.max_loaded_models}};
}


}  // namespace yunshao
