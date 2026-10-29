#pragma once

#include "common/config.hpp"

#include <filesystem>
#include <string>
#include <vector>

namespace yunshao {

struct ModelManifest {
    std::string model_id;
    std::string version;
    std::string format;
    std::string model_file;
    std::string sha256;
    std::string soc_version;
    int input_width = 0;
    int input_height = 0;
    std::vector<std::string> classes;

    static ModelManifest parse(const Json& value);
    static void validate(const ModelManifest& manifest);
    static void validate_against_profile(const ModelManifest& manifest, const ModelProfile& profile);
    Json to_json() const;
};

class ModelCatalog {
public:
    ModelCatalog(std::filesystem::path root, EdgeAppConfig config);
    void install(const std::filesystem::path& manifest_path,
                 const std::filesystem::path& model_path,
                 int device_id = 0);
    void rollback(const std::string& model_id);
    Json status(const std::string& model_id = "") const;

    static std::string sha256_file(const std::filesystem::path& path);
    static bool probe_om(const std::filesystem::path& path, int device_id, std::string* error);

private:
    std::filesystem::path root_;
    EdgeAppConfig config_;
    static void atomic_symlink(const std::filesystem::path& root,
                               const std::string& link_name,
                               const std::filesystem::path& target);
    Json status_one(const std::string& model_id) const;
};

}  // namespace yunshao
