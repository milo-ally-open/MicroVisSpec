#pragma once

#include "common/config.hpp"

#include <sqlite3.h>

#include <filesystem>
#include <mutex>
#include <optional>
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

class EventOutbox {
public:
    explicit EventOutbox(const std::filesystem::path& database_path,
                         std::string device_id,
                         EdgeAppConfig config);
    ~EventOutbox();
    EventOutbox(const EventOutbox&) = delete;
    EventOutbox& operator=(const EventOutbox&) = delete;

    bool enqueue(const Json& event, std::string* error);
    std::vector<Json> due_events(std::size_t limit = 32);
    void note_publish_attempt(const std::string& event_id, const std::string& error = "");
    bool apply_ack(const Json& ack, std::string* error);
    Json counts();
    void replace_config(EdgeAppConfig config);
    std::string event_state(const std::string& event_id);

private:
    ::sqlite3* db_ = nullptr;
    std::string device_id_;
    EdgeAppConfig config_;
    std::mutex mutex_;

    void initialize();
    void validate_event(const Json& event) const;
};

}  // namespace yunshao
