#pragma once

#include "application/ports/durable_agent_event_outbox.hpp"
#include "common/config.hpp"

#include <filesystem>
#include <mutex>

struct sqlite3;

namespace yunshao::adapters {

class SQLiteEventOutbox final : public application::ports::DurableAgentEventOutbox {
public:
    explicit SQLiteEventOutbox(const std::filesystem::path& database_path,
                               std::string device_id,
                               EdgeAppConfig config);
    ~SQLiteEventOutbox() override;
    SQLiteEventOutbox(const SQLiteEventOutbox&) = delete;
    SQLiteEventOutbox& operator=(const SQLiteEventOutbox&) = delete;

    bool enqueue(const nlohmann::json& event, std::string* error) override;
    std::vector<nlohmann::json> due_events(std::size_t limit = 32) override;
    void note_publish_attempt(const std::string& event_id,
                              const std::string& error = "") override;
    bool apply_ack(const nlohmann::json& ack, std::string* error) override;
    nlohmann::json counts() override;
    void replace_config(EdgeAppConfig config) override;
    std::string event_state(const std::string& event_id) override;

private:
    ::sqlite3* db_ = nullptr;
    std::string device_id_;
    EdgeAppConfig config_;
    std::mutex mutex_;

    void initialize();
    void validate_event(const nlohmann::json& event) const;
};

}  // namespace yunshao::adapters
