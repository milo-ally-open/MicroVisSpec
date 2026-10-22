#pragma once
#include <cstddef>
#include <cstdint>
#include <utility>
#include <vector>

namespace yunshao {
// Retain codec configuration even while the on-demand RTSP output has no reader.
class H264ParameterSets {
public:
    void observe(const std::uint8_t* data, std::size_t size) {
        auto start = [&](std::size_t from) -> std::pair<std::size_t, std::size_t> {
            for (auto i = from; i + 2 < size; ++i) {
                if (data[i] || data[i + 1]) continue;
                if (data[i + 2] == 1) return {i, i + 3};
                if (i + 3 < size && data[i + 2] == 0 && data[i + 3] == 1) return {i, i + 4};
            }
            return {size, size};
        };
        auto current = start(0);
        while (current.second < size) {
            const auto next = start(current.second);
            const auto type = data[current.second] & 31U;
            if ((type == 7 || type == 8) && next.first - current.second <= 65536) {
                auto& target = type == 7 ? sps_ : pps_;
                target = {0, 0, 0, 1};
                target.insert(target.end(), data + current.second, data + next.first);
            }
            current = next;
        }
    }
    std::vector<std::uint8_t> prefix() const {
        if (sps_.empty() || pps_.empty()) return {};
        auto result = sps_; result.insert(result.end(), pps_.begin(), pps_.end());
        return result;
    }
private:
    std::vector<std::uint8_t> sps_, pps_;
};
}
