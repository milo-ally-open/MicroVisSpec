#include "adapters/curl_https_transport.hpp"

#include <curl/curl.h>

#include <mutex>

namespace yunshao::adapters {
namespace {

size_t append_response(char* data, size_t size, size_t count, void* context) {
    if (count != 0 && size > static_cast<size_t>(-1) / count) return 0;
    const auto length = size * count;
    auto* output = static_cast<application::SensitiveString*>(context);
    constexpr std::size_t maximum_response_bytes = 65536;
    if (length > maximum_response_bytes || output->value.size() > maximum_response_bytes - length) return 0;
    output->value.append(data, length);
    return length;
}

bool initialize_curl() {
    static std::once_flag initialization_once;
    static bool initialized = false;
    std::call_once(initialization_once, [] {
        initialized = ::curl_global_init(CURL_GLOBAL_DEFAULT) == CURLE_OK;
    });
    return initialized;
}

}  // namespace

application::ports::HttpsResponse CurlHttpsTransport::perform(
    const application::ports::HttpsRequest& request,
    application::SensitiveString& response_body) {
    if (!initialize_curl())
        return {application::ports::HttpsTransportFailure::global_initialization, 0};

    CURL* curl = ::curl_easy_init();
    if (!curl) return {application::ports::HttpsTransportFailure::client_initialization, 0};

    struct curl_slist* headers = nullptr;
    if (request.body) {
        headers = ::curl_slist_append(headers, "Content-Type: application/json");
        if (!headers) {
            ::curl_easy_cleanup(curl);
            return {application::ports::HttpsTransportFailure::client_initialization, 0};
        }
    }

    ::curl_easy_setopt(curl, CURLOPT_URL, request.url.c_str());
    ::curl_easy_setopt(curl, CURLOPT_PROTOCOLS, CURLPROTO_HTTPS);
    ::curl_easy_setopt(curl, CURLOPT_CAINFO, request.ca_path.c_str());
    ::curl_easy_setopt(curl, CURLOPT_SSL_VERIFYPEER, 1L);
    ::curl_easy_setopt(curl, CURLOPT_SSL_VERIFYHOST, 2L);
    ::curl_easy_setopt(curl, CURLOPT_FOLLOWLOCATION, 0L);
    ::curl_easy_setopt(curl, CURLOPT_NOPROXY, "*");
    ::curl_easy_setopt(curl, CURLOPT_TIMEOUT, request.timeout_seconds);
    if (request.connect_timeout_seconds > 0)
        ::curl_easy_setopt(curl, CURLOPT_CONNECTTIMEOUT, request.connect_timeout_seconds);
    if (request.no_signal) ::curl_easy_setopt(curl, CURLOPT_NOSIGNAL, 1L);
    if (request.body) {
        ::curl_easy_setopt(curl, CURLOPT_POST, 1L);
        ::curl_easy_setopt(curl, CURLOPT_POSTFIELDS, request.body->data());
        ::curl_easy_setopt(curl, CURLOPT_POSTFIELDSIZE, static_cast<long>(request.body->size()));
        ::curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
    }
    if (request.username && request.password) {
        ::curl_easy_setopt(curl, CURLOPT_HTTPAUTH, CURLAUTH_BASIC);
        ::curl_easy_setopt(curl, CURLOPT_USERNAME, request.username->c_str());
        ::curl_easy_setopt(curl, CURLOPT_PASSWORD, request.password->c_str());
    }
    ::curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, append_response);
    ::curl_easy_setopt(curl, CURLOPT_WRITEDATA, &response_body);

    const CURLcode result = ::curl_easy_perform(curl);
    long status = 0;
    (void)::curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &status);
    ::curl_slist_free_all(headers);
    ::curl_easy_cleanup(curl);

    if (result != CURLE_OK)
        return {application::ports::HttpsTransportFailure::transfer, status};
    return {application::ports::HttpsTransportFailure::none, status};
}

}  // namespace yunshao::adapters
