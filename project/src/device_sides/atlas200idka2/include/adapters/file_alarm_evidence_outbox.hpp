#pragma once

#include "application/ports/alarm_evidence_outbox.hpp"

#include <filesystem>

namespace yunshao::adapters {

class FileAlarmEvidenceOutbox final : public application::ports::AlarmEvidenceOutbox {
public:
    explicit FileAlarmEvidenceOutbox(std::filesystem::path directory);

    void persist_best_effort(const std::string& event_id,
                             const std::vector<unsigned char>& jpeg) override;

private:
    std::filesystem::path directory_;
};

}  // namespace yunshao::adapters
