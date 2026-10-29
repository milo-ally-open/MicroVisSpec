#include "adapters/sqlite_event_outbox.hpp"

#include <sqlite3.h>

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <regex>
#include <stdexcept>
#include <utility>

namespace yunshao::adapters {
namespace {

using Json = nlohmann::json;

std::int64_t unix_now() {
    return std::chrono::duration_cast<std::chrono::seconds>(
               std::chrono::system_clock::now().time_since_epoch())
        .count();
}

void exec_sql(sqlite3* db, const char* sql) {
    char* message = nullptr;
    const int result = sqlite3_exec(db, sql, nullptr, nullptr, &message);
    if (result != SQLITE_OK) {
        const std::string detail = message ? message : sqlite3_errmsg(db);
        sqlite3_free(message);
        throw std::runtime_error("SQLite operation failed: " + detail);
    }
}

class Statement {
public:
    Statement(sqlite3* db, const char* sql) {
        if (sqlite3_prepare_v2(db, sql, -1, &statement_, nullptr) != SQLITE_OK) {
            throw std::runtime_error("SQLite statement preparation failed");
        }
    }
    ~Statement() { sqlite3_finalize(statement_); }
    Statement(const Statement&) = delete;
    Statement& operator=(const Statement&) = delete;
    sqlite3_stmt* get() const { return statement_; }
    void bind_text(int index, const std::string& value) {
        if (sqlite3_bind_text(statement_, index, value.c_str(), static_cast<int>(value.size()), SQLITE_TRANSIENT) != SQLITE_OK) {
            throw std::runtime_error("SQLite bind failed");
        }
    }
private:
    sqlite3_stmt* statement_ = nullptr;
};

}  // namespace

SQLiteEventOutbox::SQLiteEventOutbox(const std::filesystem::path& database_path, std::string device_id,
                                           EdgeAppConfig config)
    : device_id_(std::move(device_id)), config_(std::move(config)) {
    std::filesystem::create_directories(database_path.parent_path());
    if (sqlite3_open_v2(database_path.c_str(), &db_, SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE | SQLITE_OPEN_FULLMUTEX, nullptr) != SQLITE_OK) {
        const std::string message = db_ ? sqlite3_errmsg(db_) : "allocation failed";
        if (db_) sqlite3_close(db_);
        db_ = nullptr;
        throw std::runtime_error("cannot open local event outbox: " + message);
    }
    sqlite3_busy_timeout(db_, 5000);
    initialize();
}

SQLiteEventOutbox::~SQLiteEventOutbox() {
    if (db_) sqlite3_close(db_);
}

void SQLiteEventOutbox::initialize() {
    exec_sql(db_, "PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON;");
    exec_sql(db_, "CREATE TABLE IF NOT EXISTS event_outbox ("
                  "event_id TEXT PRIMARY KEY, device_id TEXT NOT NULL, payload TEXT NOT NULL,"
                  "state TEXT NOT NULL CHECK(state IN ('pending','acked','quarantined')),"
                  "attempts INTEGER NOT NULL DEFAULT 0, next_attempt_at INTEGER NOT NULL,"
                  "last_error TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, ack_reason TEXT NOT NULL DEFAULT '');"
                  "CREATE INDEX IF NOT EXISTS idx_event_outbox_due ON event_outbox(state,next_attempt_at,created_at);");
}

void SQLiteEventOutbox::validate_event(const Json& event) const {
    if (!event.is_object() || event.value("schema_version", 0) != 1) throw std::invalid_argument("unsupported event schema_version");
    if (event.value("device_id", "") != device_id_) throw std::invalid_argument("event device_id does not match agent identity");
    if (event.contains("account_id")) throw std::invalid_argument("edge event must not contain trusted account_id");
    const std::string event_id = event.value("event_id", "");
    if (!std::regex_match(event_id, std::regex("^[0-9a-fA-F-]{36}$"))) throw std::invalid_argument("event_id must be a UUID");
    const std::string event_type = event.value("event_type", "");
    const Json attributes = event.value("attributes", Json::object());
    const std::string algorithm_key = event.value("algorithm_key", "");
    const std::string algorithm_version = event.value("algorithm_version", "");
    const std::string model_id = attributes.value("model_id", "");
    if (algorithm_key.empty() || algorithm_version.empty()) throw std::invalid_argument("event algorithm identity is missing");
    if (model_id.empty()) throw std::invalid_argument("event model_id is missing");
    if (!attributes.contains("confidence") || !attributes["confidence"].is_number()) throw std::invalid_argument("event confidence is required");
    const double confidence = attributes["confidence"].get<double>();
    if (!std::isfinite(confidence) || confidence < 0.0 || confidence > 1.0) throw std::invalid_argument("event confidence is out of range");
    const int rule_version = attributes.value("rule_config_version", 0);
    const int observed_duration = attributes.value("observed_duration_ms", 0);
    if (rule_version != config_.alarm_config_version || observed_duration < 1) {
        throw std::invalid_argument("event rule version and duration are required");
    }

    bool matches_configured_rule = false;
    const auto model = std::find_if(config_.models.begin(), config_.models.end(), [&](const ModelProfile& candidate) {
        return candidate.model_id == model_id && candidate.algorithm_key == algorithm_key;
    });
    if (model != config_.models.end()) {
        for (const auto& rule : config_.alarm_rules) {
            if (rule.model_id != model->model_id || !rule.enabled || rule.event_type != event_type ||
                event.value("severity", "") != rule.severity || !attributes.contains(rule.class_attribute) ||
                !attributes[rule.class_attribute].is_string() ||
                attributes[rule.class_attribute].get<std::string>() != rule.class_name ||
                confidence < rule.confidence_threshold || observed_duration < rule.min_duration_ms) {
                continue;
            }
            matches_configured_rule = true;
            break;
        }
    }
    if (!matches_configured_rule) throw std::invalid_argument("event does not match an enabled configured alarm rule");
    if (event.dump().size() > config_.max_event_payload_bytes) throw std::invalid_argument("event payload exceeds configured size limit");
}

bool SQLiteEventOutbox::enqueue(const Json& event, std::string* error) {
    try {
        std::lock_guard<std::mutex> lock(mutex_);
        validate_event(event);
        Statement size_stmt(db_, "PRAGMA page_count");
        if (sqlite3_step(size_stmt.get()) != SQLITE_ROW) throw std::runtime_error("cannot inspect outbox size");
        const auto pages = sqlite3_column_int64(size_stmt.get(), 0);
        Statement page_size_stmt(db_, "PRAGMA page_size");
        if (sqlite3_step(page_size_stmt.get()) != SQLITE_ROW) throw std::runtime_error("cannot inspect outbox page size");
        const auto page_size = sqlite3_column_int64(page_size_stmt.get(), 0);
        if (static_cast<std::uint64_t>(pages * page_size) >= config_.outbox_max_bytes) throw std::runtime_error("outbox_capacity_reached");
        Statement statement(db_, "INSERT OR IGNORE INTO event_outbox(event_id,device_id,payload,state,next_attempt_at,created_at) VALUES(?,?,?,'pending',?,?)");
        statement.bind_text(1, event.at("event_id").get<std::string>());
        statement.bind_text(2, device_id_);
        statement.bind_text(3, event.dump());
        sqlite3_bind_int64(statement.get(), 4, unix_now());
        sqlite3_bind_int64(statement.get(), 5, unix_now());
        if (sqlite3_step(statement.get()) != SQLITE_DONE) throw std::runtime_error("cannot persist event to outbox");
        return true;
    } catch (const std::exception& exception) {
        if (error) *error = exception.what();
        return false;
    }
}

void SQLiteEventOutbox::replace_config(EdgeAppConfig config) {
    std::lock_guard<std::mutex> lock(mutex_);
    config_ = std::move(config);
}

std::string SQLiteEventOutbox::event_state(const std::string& event_id) {
    std::lock_guard<std::mutex> lock(mutex_);
    Statement statement(db_, "SELECT state FROM event_outbox WHERE event_id=?");
    statement.bind_text(1, event_id);
    if (sqlite3_step(statement.get()) != SQLITE_ROW) return "";
    return reinterpret_cast<const char*>(sqlite3_column_text(statement.get(), 0));
}

std::vector<Json> SQLiteEventOutbox::due_events(std::size_t limit) {
    std::lock_guard<std::mutex> lock(mutex_);
    Statement statement(db_, "SELECT payload FROM event_outbox WHERE state='pending' AND next_attempt_at<=? ORDER BY created_at LIMIT ?");
    sqlite3_bind_int64(statement.get(), 1, unix_now());
    sqlite3_bind_int64(statement.get(), 2, static_cast<sqlite3_int64>(limit));
    std::vector<Json> result;
    int status = SQLITE_ROW;
    while ((status = sqlite3_step(statement.get())) == SQLITE_ROW) {
        const auto* text = reinterpret_cast<const char*>(sqlite3_column_text(statement.get(), 0));
        result.push_back(Json::parse(text ? text : "{}"));
    }
    if (status != SQLITE_DONE) throw std::runtime_error("cannot read pending outbox events");
    return result;
}

void SQLiteEventOutbox::note_publish_attempt(const std::string& event_id, const std::string& error) {
    std::lock_guard<std::mutex> lock(mutex_);
    Statement statement(db_, "UPDATE event_outbox SET attempts=attempts+1,next_attempt_at=?,last_error=? WHERE event_id=? AND state='pending'");
    const auto now = unix_now();
    Statement current(db_, "SELECT attempts FROM event_outbox WHERE event_id=? AND state='pending'");
    current.bind_text(1, event_id);
    int attempts = 0;
    if (sqlite3_step(current.get()) == SQLITE_ROW) attempts = sqlite3_column_int(current.get(), 0) + 1;
    int backoff = config_.event_retry_base_seconds;
    for (int retry = 1; retry < attempts && backoff < config_.event_retry_max_seconds; ++retry) {
        backoff = static_cast<int>(std::min<std::int64_t>(config_.event_retry_max_seconds,
                                                           static_cast<std::int64_t>(backoff) * 2));
    }
    sqlite3_bind_int64(statement.get(), 1, now + backoff);
    statement.bind_text(2, error);
    statement.bind_text(3, event_id);
    if (sqlite3_step(statement.get()) != SQLITE_DONE) throw std::runtime_error("cannot update outbox retry state");
}

bool SQLiteEventOutbox::apply_ack(const Json& ack, std::string* error) {
    try {
        if (!ack.is_object() || ack.value("schema_version", 0) != 1) throw std::invalid_argument("unsupported ack schema_version");
        if (ack.value("device_id", "") != device_id_) throw std::invalid_argument("ack device_id does not match agent identity");
        const auto event_id = ack.value("event_id", "");
        const auto status = ack.value("result", ack.value("status", ""));
        if (event_id.empty()) throw std::invalid_argument("ack event_id is missing");
        std::lock_guard<std::mutex> lock(mutex_);
        Statement statement(db_, "UPDATE event_outbox SET state=?,ack_reason=?,last_error='' WHERE event_id=? AND device_id=? AND state='pending'");
        if (status == "accepted" || status == "duplicate") {
            statement.bind_text(1, "acked");
            statement.bind_text(2, "");
        } else if (status == "rejected") {
            const auto reason = ack.value("reason_code", "");
            if (reason.empty()) throw std::invalid_argument("rejected ack needs reason_code");
            statement.bind_text(1, "quarantined");
            statement.bind_text(2, reason);
        } else {
            throw std::invalid_argument("ack status is invalid");
        }
        statement.bind_text(3, event_id);
        statement.bind_text(4, device_id_);
        if (sqlite3_step(statement.get()) != SQLITE_DONE) throw std::runtime_error("cannot apply event ack");
        return sqlite3_changes(db_) == 1;
    } catch (const std::exception& exception) {
        if (error) *error = exception.what();
        return false;
    }
}

Json SQLiteEventOutbox::counts() {
    std::lock_guard<std::mutex> lock(mutex_);
    Statement statement(db_, "SELECT state,count(*) FROM event_outbox GROUP BY state");
    Json result{{"pending", 0}, {"acked", 0}, {"quarantined", 0}};
    while (sqlite3_step(statement.get()) == SQLITE_ROW) {
        const auto* state = reinterpret_cast<const char*>(sqlite3_column_text(statement.get(), 0));
        if (state) result[state] = sqlite3_column_int64(statement.get(), 1);
    }
    return result;
}

}  // namespace yunshao::adapters
