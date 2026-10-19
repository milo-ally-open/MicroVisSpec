#pragma once

#include <memory>
#include <string>
#include <vector>

struct RegionOsdDetection {
    int class_id = -1;
    float confidence = 0.0F;
    int x = 0;
    int y = 0;
    int width = 0;
    int height = 0;
};

class RegionOsd {
public:
    RegionOsd(int group, int channel, int width, int height);
    RegionOsd(const RegionOsd&) = delete;
    RegionOsd& operator=(const RegionOsd&) = delete;
    ~RegionOsd();

    void update(const std::vector<RegionOsdDetection>& detections,
                const std::vector<std::string>& labels);

private:
    class Impl;
    std::unique_ptr<Impl> impl_;
};
