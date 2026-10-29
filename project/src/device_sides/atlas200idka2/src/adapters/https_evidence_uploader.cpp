#include "adapters/https_evidence_uploader.hpp"

#include <curl/curl.h>

#include <cstdio>
#include <filesystem>
#include <iostream>
#include <mutex>
#include <stdexcept>
#include <utility>

namespace yunshao::adapters {
namespace {

std::size_t discard_response(char*, std::size_t size, std::size_t count, void*) {
    return size * count;
}

}  // namespace

HttpsEvidenceUploader::HttpsEvidenceUploader(
    application::ports::DurableAgentEventOutbox& event_outbox,
    HttpsEvidenceUploadSettings settings)
    : event_outbox_(event_outbox), settings_(std::move(settings)) {}

void HttpsEvidenceUploader::upload_acknowledged(const std::string& device_id) {
    if (settings_.api_origin.empty()) return;
    if (settings_.api_origin.rfind("https://", 0) != 0) {
        throw std::runtime_error("edge API must use HTTPS");
    }
    if (!std::filesystem::exists(settings_.evidence_directory)) return;

    static std::once_flag curl_initialized;
    std::call_once(curl_initialized, [] {
        if (curl_global_init(CURL_GLOBAL_DEFAULT) != CURLE_OK) {
            throw std::runtime_error("cannot initialize HTTPS uploader");
        }
    });

    for (const auto& entry : std::filesystem::directory_iterator(settings_.evidence_directory)) {
        if (!entry.is_regular_file() || entry.path().extension() != ".jpg") continue;
        const auto event_id = entry.path().stem().string();
        if (event_id.size() != 36) continue;

        const auto state = event_outbox_.event_state(event_id);
        if (state == "quarantined") {
            std::filesystem::remove(entry.path());
            continue;
        }
        if (state != "acked") continue;

        const auto bytes = entry.file_size();
        if (bytes == 0 || bytes > 4 * 1024 * 1024) continue;
        FILE* file = std::fopen(entry.path().c_str(), "rb");
        if (!file) return;
        CURL* curl = curl_easy_init();
        if (!curl) {
            std::fclose(file);
            return;
        }

        const auto url = settings_.api_origin + "/edge/v1/devices/" + device_id +
                         "/events/" + event_id + "/evidence";
        const auto ca_path = settings_.ca_path.string();
        auto* headers = curl_slist_append(nullptr, "Content-Type: image/jpeg");
        curl_easy_setopt(curl, CURLOPT_URL, url.c_str());
        curl_easy_setopt(curl, CURLOPT_PROXY, "");
        curl_easy_setopt(curl, CURLOPT_UPLOAD, 1L);
        curl_easy_setopt(curl, CURLOPT_READDATA, file);
        curl_easy_setopt(curl, CURLOPT_INFILESIZE_LARGE, static_cast<curl_off_t>(bytes));
        curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
        curl_easy_setopt(curl, CURLOPT_HTTPAUTH, CURLAUTH_BASIC);
        curl_easy_setopt(curl, CURLOPT_USERNAME, settings_.username.c_str());
        curl_easy_setopt(curl, CURLOPT_PASSWORD, settings_.password.c_str());
        curl_easy_setopt(curl, CURLOPT_CAINFO, ca_path.c_str());
        curl_easy_setopt(curl, CURLOPT_SSL_VERIFYPEER, 1L);
        curl_easy_setopt(curl, CURLOPT_SSL_VERIFYHOST, 2L);
        curl_easy_setopt(curl, CURLOPT_PROTOCOLS, CURLPROTO_HTTPS);
        curl_easy_setopt(curl, CURLOPT_FOLLOWLOCATION, 0L);
        curl_easy_setopt(curl, CURLOPT_CONNECTTIMEOUT, 3L);
        curl_easy_setopt(curl, CURLOPT_TIMEOUT, 8L);
        curl_easy_setopt(curl, CURLOPT_NOSIGNAL, 1L);
        curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, &discard_response);

        const auto result = curl_easy_perform(curl);
        long status = 0;
        curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &status);
        curl_slist_free_all(headers);
        curl_easy_cleanup(curl);
        std::fclose(file);

        if (result == CURLE_OK && status >= 200 && status < 300) {
            std::filesystem::remove(entry.path());
            std::cout << "evidence_uploaded event_id=" << event_id << '\n';
        }
        return;  // Bound work per heartbeat; failures retry on a later pass.
    }
}

}  // namespace yunshao::adapters
