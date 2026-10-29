#pragma once

#include "application/ports/durable_agent_event_outbox.hpp"
#include "application/ports/evidence_uploader.hpp"

#include <filesystem>
#include <string>

namespace yunshao::adapters {

struct HttpsEvidenceUploadSettings {
    std::string api_origin;
    std::filesystem::path evidence_directory;
    std::string username;
    std::string password;
    std::filesystem::path ca_path;
};

class HttpsEvidenceUploader final : public application::ports::EvidenceUploader {
public:
    HttpsEvidenceUploader(application::ports::DurableAgentEventOutbox& event_outbox,
                          HttpsEvidenceUploadSettings settings);

    void upload_acknowledged(const std::string& device_id) override;

private:
    application::ports::DurableAgentEventOutbox& event_outbox_;
    HttpsEvidenceUploadSettings settings_;
};

}  // namespace yunshao::adapters
