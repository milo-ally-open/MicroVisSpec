#include <acl/acl.h>
#include <opencv2/core.hpp>
#include <opencv2/dnn.hpp>
#include <opencv2/imgcodecs.hpp>
#include <opencv2/imgproc.hpp>
#include <opencv2/videoio.hpp>

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdlib>
#include <fstream>
#include <iomanip>
#include <iostream>
#include <numeric>
#include <stdexcept>
#include <string>
#include <sys/resource.h>
#include <vector>

namespace {

constexpr int kInputWidth = 640;
constexpr int kInputHeight = 640;
constexpr int kWarmup = 5;

struct Options {
    std::string model;
    std::string video;
    std::string output = "detected.mp4";
    std::string labels = "classes.txt";
    float confidence = 0.25F;
    float iou = 0.45F;
    int device = 0;
};

struct LetterboxInfo {
    float scale;
    int left;
    int top;
    int width;
    int height;
};

struct Detection {
    int class_id;
    float confidence;
    cv::Rect box;
};

void check_acl(aclError code, const char* expression) {
    if (code != ACL_SUCCESS) {
        throw std::runtime_error(std::string(expression) + " failed with ACL error " + std::to_string(code));
    }
}

#define ACL_CHECK(expression) check_acl((expression), #expression)

Options parse_options(int argc, char** argv) {
    Options options;
    for (int i = 1; i < argc; ++i) {
        const std::string arg = argv[i];
        auto value = [&](const char* name) -> std::string {
            if (i + 1 >= argc) throw std::invalid_argument(std::string("missing value for ") + name);
            return argv[++i];
        };
        if (arg == "--model") options.model = value("--model");
        else if (arg == "--video") options.video = value("--video");
        else if (arg == "--output") options.output = value("--output");
        else if (arg == "--labels") options.labels = value("--labels");
        else if (arg == "--conf") options.confidence = std::stof(value("--conf"));
        else if (arg == "--iou") options.iou = std::stof(value("--iou"));
        else if (arg == "--device") options.device = std::stoi(value("--device"));
        else if (arg == "--help" || arg == "-h") {
            std::cout << "Usage: atlas_yolo_acl_demo --model model.om --video input.mp4 [--output detected.mp4] [--labels classes.txt] [--conf 0.25] [--iou 0.45] [--device 0]\n";
            std::exit(0);
        } else throw std::invalid_argument("unknown option: " + arg);
    }
    if (options.model.empty() || options.video.empty()) throw std::invalid_argument("--model and --video are required");
    if (options.confidence < 0.0F || options.confidence > 1.0F) throw std::invalid_argument("--conf must be within [0,1]");
    if (options.iou < 0.0F || options.iou > 1.0F) throw std::invalid_argument("--iou must be within [0,1]");
    if (options.device < 0) throw std::invalid_argument("--device must be >=0");
    return options;
}

std::vector<std::string> read_labels(const std::string& path) {
    std::ifstream input(path);
    if (!input) throw std::runtime_error("cannot open labels file: " + path);
    std::vector<std::string> labels;
    for (std::string line; std::getline(input, line);) {
        if (!line.empty() && line.back() == '\r') line.pop_back();
        if (!line.empty()) labels.push_back(line);
    }
    if (labels.empty()) throw std::runtime_error("labels file is empty: " + path);
    return labels;
}

cv::Mat preprocess(const cv::Mat& image, LetterboxInfo& info) {
    const float scale = std::min(static_cast<float>(kInputWidth) / image.cols,
                                 static_cast<float>(kInputHeight) / image.rows);
    const int resized_width = static_cast<int>(std::round(image.cols * scale));
    const int resized_height = static_cast<int>(std::round(image.rows * scale));
    const int horizontal = kInputWidth - resized_width;
    const int vertical = kInputHeight - resized_height;
    const int left = static_cast<int>(std::round(horizontal / 2.0F - 0.1F));
    const int right = static_cast<int>(std::round(horizontal / 2.0F + 0.1F));
    const int top = static_cast<int>(std::round(vertical / 2.0F - 0.1F));
    const int bottom = static_cast<int>(std::round(vertical / 2.0F + 0.1F));
    cv::Mat resized;
    cv::resize(image, resized, cv::Size(resized_width, resized_height), 0, 0, cv::INTER_LINEAR);
    cv::Mat canvas;
    cv::copyMakeBorder(resized, canvas, top, bottom, left, right, cv::BORDER_CONSTANT, cv::Scalar(114, 114, 114));
    cv::Mat blob;
    cv::dnn::blobFromImage(canvas, blob, 1.0 / 255.0, cv::Size(kInputWidth, kInputHeight), cv::Scalar(), true, false, CV_32F);
    info = {scale, left, top, resized_width, resized_height};
    return blob;
}

double resident_megabytes() {
    rusage usage{};
    if (getrusage(RUSAGE_SELF, &usage) != 0) return 0.0;
    return static_cast<double>(usage.ru_maxrss) / 1024.0;
}

class AclModel {
public:
    explicit AclModel(const Options& options) : device_(options.device) {
        ACL_CHECK(aclInit(nullptr));
        initialized_ = true;
        ACL_CHECK(aclrtSetDevice(device_));
        device_set_ = true;
        ACL_CHECK(aclmdlLoadFromFile(options.model.c_str(), &model_id_));
        loaded_ = true;
        descriptor_ = aclmdlCreateDesc();
        if (descriptor_ == nullptr) throw std::runtime_error("aclmdlCreateDesc returned null");
        ACL_CHECK(aclmdlGetDesc(descriptor_, model_id_));
        if (aclmdlGetNumInputs(descriptor_) != 1 || aclmdlGetNumOutputs(descriptor_) != 1) {
            throw std::runtime_error("expected exactly one model input and one output");
        }
        input_size_ = aclmdlGetInputSizeByIndex(descriptor_, 0);
        output_size_ = aclmdlGetOutputSizeByIndex(descriptor_, 0);
        if (input_size_ != static_cast<size_t>(3 * kInputWidth * kInputHeight * sizeof(float))) {
            throw std::runtime_error("model input is not float32 NCHW 1x3x640x640");
        }
        ACL_CHECK(aclrtMalloc(&input_device_, input_size_, ACL_MEM_MALLOC_HUGE_FIRST));
        ACL_CHECK(aclrtMalloc(&output_device_, output_size_, ACL_MEM_MALLOC_HUGE_FIRST));
        input_buffer_ = aclCreateDataBuffer(input_device_, input_size_);
        output_buffer_ = aclCreateDataBuffer(output_device_, output_size_);
        input_dataset_ = aclmdlCreateDataset();
        output_dataset_ = aclmdlCreateDataset();
        if (!input_buffer_ || !output_buffer_ || !input_dataset_ || !output_dataset_) throw std::runtime_error("failed to allocate ACL datasets");
        ACL_CHECK(aclmdlAddDatasetBuffer(input_dataset_, input_buffer_));
        ACL_CHECK(aclmdlAddDatasetBuffer(output_dataset_, output_buffer_));
        aclmdlIODims dims{};
        ACL_CHECK(aclmdlGetOutputDims(descriptor_, 0, &dims));
        if (dims.dimCount != 3 || dims.dims[0] != 1) throw std::runtime_error("expected a rank-3 batch-1 YOLO output");
        const int first_axis = static_cast<int>(dims.dims[1]);
        const int second_axis = static_cast<int>(dims.dims[2]);
        channels_first_ = first_axis < second_axis;
        channels_ = channels_first_ ? first_axis : second_axis;
        candidates_ = channels_first_ ? second_axis : first_axis;
        if (aclmdlGetOutputDataType(descriptor_, 0) != ACL_FLOAT) throw std::runtime_error("model output must be float32");
        std::cout << "Model output: [1," << dims.dims[1] << ',' << dims.dims[2] << "]\n";
    }

    ~AclModel() { release(); }

    const float* infer(const cv::Mat& blob, std::vector<float>& host_output) {
        ACL_CHECK(aclrtMemcpy(input_device_, input_size_, blob.ptr<float>(), input_size_, ACL_MEMCPY_HOST_TO_DEVICE));
        ACL_CHECK(aclmdlExecute(model_id_, input_dataset_, output_dataset_));
        host_output.resize(output_size_ / sizeof(float));
        ACL_CHECK(aclrtMemcpy(host_output.data(), output_size_, output_device_, output_size_, ACL_MEMCPY_DEVICE_TO_HOST));
        return host_output.data();
    }

    int channels() const { return channels_; }
    int candidates() const { return candidates_; }
    bool channels_first() const { return channels_first_; }

private:
    void release() noexcept {
        if (input_dataset_) aclmdlDestroyDataset(input_dataset_);
        if (output_dataset_) aclmdlDestroyDataset(output_dataset_);
        if (input_buffer_) aclDestroyDataBuffer(input_buffer_);
        if (output_buffer_) aclDestroyDataBuffer(output_buffer_);
        if (input_device_) aclrtFree(input_device_);
        if (output_device_) aclrtFree(output_device_);
        if (descriptor_) aclmdlDestroyDesc(descriptor_);
        if (loaded_) aclmdlUnload(model_id_);
        if (device_set_) aclrtResetDevice(device_);
        if (initialized_) aclFinalize();
    }

    int device_ = 0;
    uint32_t model_id_ = 0;
    size_t input_size_ = 0;
    size_t output_size_ = 0;
    void* input_device_ = nullptr;
    void* output_device_ = nullptr;
    aclmdlDesc* descriptor_ = nullptr;
    aclmdlDataset* input_dataset_ = nullptr;
    aclmdlDataset* output_dataset_ = nullptr;
    aclDataBuffer* input_buffer_ = nullptr;
    aclDataBuffer* output_buffer_ = nullptr;
    bool initialized_ = false;
    bool device_set_ = false;
    bool loaded_ = false;
    bool channels_first_ = true;
    int channels_ = 0;
    int candidates_ = 0;
};

std::vector<Detection> postprocess(const std::vector<float>& output, const AclModel& model,
                                  const std::vector<std::string>& labels, const Options& options,
                                  const LetterboxInfo& letterbox, const cv::Size& image_size) {
    const int class_offset = model.channels() == static_cast<int>(labels.size()) + 5 ? 5 : 4;
    if (model.channels() != static_cast<int>(labels.size()) + 4 && class_offset != 5) {
        throw std::runtime_error("output channel count does not match labels; verify class order/count");
    }
    auto at = [&](int channel, int candidate) -> float {
        return model.channels_first()
            ? output[static_cast<size_t>(channel) * model.candidates() + candidate]
            : output[static_cast<size_t>(candidate) * model.channels() + channel];
    };
    std::vector<std::vector<cv::Rect>> boxes(labels.size());
    std::vector<std::vector<float>> scores(labels.size());
    for (int i = 0; i < model.candidates(); ++i) {
        float objectness = class_offset == 5 ? at(4, i) : 1.0F;
        int best_class = -1;
        float best_score = 0.0F;
        for (int c = 0; c < static_cast<int>(labels.size()); ++c) {
            const float score = objectness * at(class_offset + c, i);
            if (score > best_score) { best_score = score; best_class = c; }
        }
        if (best_class < 0 || best_score < options.confidence) continue;
        const float cx = at(0, i);
        const float cy = at(1, i);
        const float width = at(2, i);
        const float height = at(3, i);
        const int x0 = static_cast<int>(std::floor((cx - width * 0.5F - letterbox.left) / letterbox.scale));
        const int y0 = static_cast<int>(std::floor((cy - height * 0.5F - letterbox.top) / letterbox.scale));
        const int x1 = static_cast<int>(std::ceil((cx + width * 0.5F - letterbox.left) / letterbox.scale));
        const int y1 = static_cast<int>(std::ceil((cy + height * 0.5F - letterbox.top) / letterbox.scale));
        const int left = std::clamp(x0, 0, image_size.width - 1);
        const int top = std::clamp(y0, 0, image_size.height - 1);
        const int right = std::clamp(x1, left + 1, image_size.width);
        const int bottom = std::clamp(y1, top + 1, image_size.height);
        boxes[best_class].emplace_back(left, top, right - left, bottom - top);
        scores[best_class].push_back(best_score);
    }
    std::vector<Detection> detections;
    for (int c = 0; c < static_cast<int>(labels.size()); ++c) {
        std::vector<int> kept;
        cv::dnn::NMSBoxes(boxes[c], scores[c], options.confidence, options.iou, kept);
        for (int index : kept) detections.push_back({c, scores[c][index], boxes[c][index]});
    }
    std::sort(detections.begin(), detections.end(), [](const Detection& a, const Detection& b) { return a.confidence > b.confidence; });
    return detections;
}

double percentile(std::vector<double> values, double p) {
    if (values.empty()) return 0.0;
    std::sort(values.begin(), values.end());
    return values[static_cast<size_t>(std::ceil(p * values.size()) - 1)];
}

void draw_detections(cv::Mat& frame, const std::vector<Detection>& detections,
                     const std::vector<std::string>& labels) {
    for (const auto& detection : detections) {
        cv::rectangle(frame, detection.box, cv::Scalar(0, 220, 120), 2);
        const std::string text = labels[detection.class_id] + " " + cv::format("%.2f", detection.confidence);
        const int y = std::max(18, detection.box.y - 6);
        cv::putText(frame, text, cv::Point(detection.box.x, y), cv::FONT_HERSHEY_SIMPLEX,
                    0.55, cv::Scalar(0, 220, 120), 2, cv::LINE_AA);
    }
}

}

int main(int argc, char** argv) {
    try {
        const Options options = parse_options(argc, argv);
        const auto labels = read_labels(options.labels);
        cv::VideoCapture video(options.video);
        if (!video.isOpened()) throw std::runtime_error("cannot open video: " + options.video);
        cv::Mat frame;
        if (!video.read(frame) || frame.empty()) throw std::runtime_error("video has no decodable frames: " + options.video);
        const double source_fps = video.get(cv::CAP_PROP_FPS);
        const double fps = source_fps > 0.0 && std::isfinite(source_fps) ? source_fps : 25.0;
        const int width = frame.cols;
        const int height = frame.rows;
        cv::VideoWriter writer(options.output, cv::VideoWriter::fourcc('m', 'p', '4', 'v'),
                               fps, cv::Size(width, height));
        if (!writer.isOpened()) throw std::runtime_error("cannot create output video: " + options.output);
        AclModel model(options);
        if (model.channels() < static_cast<int>(labels.size()) + 4) throw std::runtime_error("invalid YOLO output shape");
        std::vector<float> output;
        LetterboxInfo warmup_letterbox{};
        const cv::Mat warmup_blob = preprocess(frame, warmup_letterbox);
        for (int i = 0; i < kWarmup; ++i) model.infer(warmup_blob, output);
        std::vector<double> preprocess_durations, infer_durations, postprocess_durations;
        size_t frames = 0;
        do {
            LetterboxInfo letterbox{};
            const auto preprocess_start = std::chrono::steady_clock::now();
            const cv::Mat blob = preprocess(frame, letterbox);
            const auto preprocess_end = std::chrono::steady_clock::now();
            const auto infer_start = std::chrono::steady_clock::now();
            model.infer(blob, output);
            const auto infer_end = std::chrono::steady_clock::now();
            const auto post_start = std::chrono::steady_clock::now();
            const auto detections = postprocess(output, model, labels, options, letterbox, frame.size());
            draw_detections(frame, detections, labels);
            writer.write(frame);
            const auto post_end = std::chrono::steady_clock::now();
            preprocess_durations.push_back(std::chrono::duration<double, std::milli>(preprocess_end - preprocess_start).count());
            infer_durations.push_back(std::chrono::duration<double, std::milli>(infer_end - infer_start).count());
            postprocess_durations.push_back(std::chrono::duration<double, std::milli>(post_end - post_start).count());
            ++frames;
        } while (video.read(frame) && !frame.empty());
        const auto mean = [](const std::vector<double>& values) {
            return std::accumulate(values.begin(), values.end(), 0.0) / values.size();
        };
        std::cout << std::fixed << std::setprecision(3);
        std::cout << "Input video: " << options.video << " (" << width << 'x' << height << ", " << fps << " FPS)\n";
        std::cout << "Output video: " << options.output << "\nFrames processed: " << frames << '\n';
        std::cout << "Timing ms/frame: preprocess=" << mean(preprocess_durations)
                  << " inference_mean=" << mean(infer_durations)
                  << " inference_p50=" << percentile(infer_durations, 0.50)
                  << " inference_p95=" << percentile(infer_durations, 0.95)
                  << " postprocess=" << mean(postprocess_durations) << '\n';
        std::cout << "Peak RSS MiB: " << resident_megabytes() << "\n";
        return 0;
    } catch (const std::exception& error) {
        std::cerr << "ERROR: " << error.what() << '\n';
        return 1;
    }
}
