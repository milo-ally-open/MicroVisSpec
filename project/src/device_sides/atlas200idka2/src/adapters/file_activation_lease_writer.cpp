#include "adapters/file_activation_lease_writer.hpp"

#include <cerrno>
#include <grp.h>
#include <pwd.h>
#include <stdexcept>
#include <string>
#include <fcntl.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>
#include <utility>

namespace yunshao::adapters {

FileActivationLeaseWriter::FileActivationLeaseWriter(std::filesystem::path lease_path)
    : lease_path_(std::move(lease_path)) {}

void FileActivationLeaseWriter::write_expiry_unix(std::optional<std::int64_t> expires_at_unix) {
    if (expires_at_unix && *expires_at_unix < 0)
        throw std::runtime_error("设备授权期限超出支持范围");

    const auto temporary = lease_path_.string() + ".tmp." + std::to_string(::getpid());
    const int descriptor = ::open(temporary.c_str(), O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC | O_NOFOLLOW, 0600);
    if (descriptor < 0) throw std::runtime_error("无法保存设备授权期限");
    if (::geteuid() == 0) {
        const auto* user = ::getpwnam("yunshao");
        const auto* group = ::getgrnam("yunshao");
        if (!user || !group || ::fchown(descriptor, user->pw_uid, group->gr_gid) != 0 || ::fchmod(descriptor, 0640) != 0) {
            (void)::close(descriptor);
            (void)::unlink(temporary.c_str());
            throw std::runtime_error("无法设置设备授权期限文件权限");
        }
    } else if (::fchmod(descriptor, 0600) != 0) {
        (void)::close(descriptor);
        (void)::unlink(temporary.c_str());
        throw std::runtime_error("无法设置设备授权期限文件权限");
    }

    const std::string contents = expires_at_unix
        ? "{\"expires_at_unix\":" + std::to_string(*expires_at_unix) + "}\n"
        : "{\"expires_at_unix\":null}\n";
    std::size_t offset = 0;
    while (offset < contents.size()) {
        const auto written = ::write(descriptor, contents.data() + offset, contents.size() - offset);
        if (written < 0 && errno == EINTR) continue;
        if (written <= 0) {
            (void)::close(descriptor);
            (void)::unlink(temporary.c_str());
            throw std::runtime_error("无法写入设备授权期限");
        }
        offset += static_cast<std::size_t>(written);
    }

    const int sync_result = ::fsync(descriptor);
    const int close_result = ::close(descriptor);
    if (sync_result != 0 || close_result != 0 || ::rename(temporary.c_str(), lease_path_.c_str()) != 0) {
        (void)::unlink(temporary.c_str());
        throw std::runtime_error("无法提交设备授权期限");
    }
}

}  // namespace yunshao::adapters
