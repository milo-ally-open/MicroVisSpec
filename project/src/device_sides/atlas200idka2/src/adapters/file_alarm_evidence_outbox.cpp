#include "adapters/file_alarm_evidence_outbox.hpp"

#include <cstdint>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <utility>

namespace yunshao::adapters {

FileAlarmEvidenceOutbox::FileAlarmEvidenceOutbox(std::filesystem::path directory)
    : directory_(std::move(directory)) {}

void FileAlarmEvidenceOutbox::persist_best_effort(const std::string& event_id,
                                                   const std::vector<unsigned char>& jpeg) {
    if (jpeg.empty() || jpeg.size() > 4 * 1024 * 1024) return;

    const auto evidence = directory_ / (event_id + ".jpg");
    try {
        std::filesystem::create_directories(directory_);
        std::uintmax_t used = 0;
        for (const auto& item : std::filesystem::directory_iterator(directory_)) {
            if (item.is_regular_file()) used += item.file_size();
        }
        if (used + jpeg.size() > 100 * 1024 * 1024) {
            throw std::runtime_error("evidence outbox is full");
        }
        auto temporary = evidence;
        temporary += ".tmp";
        {
            std::ofstream output(temporary, std::ios::binary);
            output.write(reinterpret_cast<const char*>(jpeg.data()), static_cast<std::streamsize>(jpeg.size()));
            output.flush();
            if (!output) throw std::runtime_error("cannot persist evidence");
        }
        std::filesystem::rename(temporary, evidence);
    } catch (const std::exception&) {
        std::cerr << "alarm_evidence_persist_failed\n";
    }
}

}  // namespace yunshao::adapters
