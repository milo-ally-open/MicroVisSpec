#pragma once

#include <cstddef>
#include <string>
#include <utility>

namespace yunshao::application {

class SensitiveString {
public:
    std::string value;

    SensitiveString() = default;
    explicit SensitiveString(std::string input) : value(std::move(input)) {}
    SensitiveString(const SensitiveString&) = delete;
    SensitiveString& operator=(const SensitiveString&) = delete;

    SensitiveString(SensitiveString&& other) noexcept : value(std::move(other.value)) {
        wipe(other.value);
    }

    SensitiveString& operator=(SensitiveString&& other) noexcept {
        if (this != &other) {
            wipe(value);
            value = std::move(other.value);
            wipe(other.value);
        }
        return *this;
    }

    ~SensitiveString() { wipe(value); }

private:
    static void wipe(std::string& text) noexcept {
        volatile char* data = text.empty() ? nullptr : text.data();
        for (std::size_t index = 0; index < text.size(); ++index) data[index] = '\0';
    }
};

}  // namespace yunshao::application
