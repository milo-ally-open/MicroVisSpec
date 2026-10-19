#include "ai_interface.hpp"

#include <acl/acl.h>
#include <opencv2/core.hpp>
#include <opencv2/dnn.hpp>

#include <algorithm>
#include <cmath>
#include <cstdint>
#include <memory>
#include <stdexcept>
#include <string>
#include <vector>

namespace yunshao {
namespace {

void check_acl(aclError code, const char* operation) {
    if (code != ACL_SUCCESS) {
        throw std::runtime_error(std::string(operation) + " failed with ACL error " + std::to_string(code));
    }
}

class AscendModel {
public:
    AscendModel(const std::string& path, int device, const ModelProfile& profile)
        : device_(device), input_width_(profile.input_width), input_height_(profile.input_height),
          pixel_format_(profile.pixel_format) {
        try {
            if (pixel_format_ != "nv21") throw std::invalid_argument("YOLO11 AscendCL plugin supports NV21 input only");
            check_acl(aclInit(nullptr), "aclInit");
            initialized_ = true;
            check_acl(aclrtSetDevice(device_), "aclrtSetDevice");
            device_set_ = true;
            check_acl(aclmdlLoadFromFile(path.c_str(), &model_id_), "aclmdlLoadFromFile");
            loaded_ = true;
            descriptor_ = aclmdlCreateDesc();
            if (descriptor_ == nullptr) throw std::runtime_error("aclmdlCreateDesc returned null");
            check_acl(aclmdlGetDesc(descriptor_, model_id_), "aclmdlGetDesc");
            if (aclmdlGetNumInputs(descriptor_) != 1 || aclmdlGetNumOutputs(descriptor_) != 1) {
                throw std::runtime_error("YOLO11 OM must have one input and one output");
            }
            input_size_ = aclmdlGetInputSizeByIndex(descriptor_, 0);
            output_size_ = aclmdlGetOutputSizeByIndex(descriptor_, 0);
            const size_t expected_nv21 = static_cast<size_t>(input_width_) * input_height_ * 3 / 2;
            if (input_size_ != expected_nv21 || aclmdlGetInputDataType(descriptor_, 0) != ACL_UINT8) {
                throw std::runtime_error("model input tensor does not match the configured static-AIPP NV21 dimensions");
            }
            if (output_size_ == 0 || output_size_ % sizeof(float) != 0 ||
                aclmdlGetOutputDataType(descriptor_, 0) != ACL_FLOAT) {
                throw std::runtime_error("model output must be non-empty float32 tensor data");
            }
            aclmdlIODims dims{};
            check_acl(aclmdlGetOutputDims(descriptor_, 0, &dims), "aclmdlGetOutputDims");
            if (dims.dimCount != 3 || dims.dims[0] != 1) {
                throw std::runtime_error("expected rank-3 batch-1 YOLO output");
            }
            const int axis_one = static_cast<int>(dims.dims[1]);
            const int axis_two = static_cast<int>(dims.dims[2]);
            channels_first_ = axis_one < axis_two;
            channels_ = channels_first_ ? axis_one : axis_two;
            candidates_ = channels_first_ ? axis_two : axis_one;
            check_acl(aclrtMalloc(&input_device_, input_size_, ACL_MEM_MALLOC_HUGE_FIRST), "aclrtMalloc(input)");
            check_acl(aclrtMalloc(&output_device_, output_size_, ACL_MEM_MALLOC_HUGE_FIRST), "aclrtMalloc(output)");
            input_buffer_ = aclCreateDataBuffer(input_device_, input_size_);
            output_buffer_ = aclCreateDataBuffer(output_device_, output_size_);
            input_dataset_ = aclmdlCreateDataset();
            output_dataset_ = aclmdlCreateDataset();
            if (input_buffer_ == nullptr || output_buffer_ == nullptr || input_dataset_ == nullptr || output_dataset_ == nullptr) {
                throw std::runtime_error("failed to allocate ACL model datasets");
            }
            check_acl(aclmdlAddDatasetBuffer(input_dataset_, input_buffer_), "aclmdlAddDatasetBuffer(input)");
            check_acl(aclmdlAddDatasetBuffer(output_dataset_, output_buffer_), "aclmdlAddDatasetBuffer(output)");
        } catch (...) {
            release();
            throw;
        }
    }

    ~AscendModel() { release(); }

    void infer(const InferenceFrame& frame, std::vector<float>& output) {
        const auto required = static_cast<size_t>(frame.stride_width) * static_cast<size_t>(frame.stride_height) * 3 / 2;
        if (frame.nv21_device == nullptr || frame.width != input_width_ || frame.height != input_height_ ||
            frame.stride_width != input_width_ || frame.stride_height != input_height_ || frame.bytes != required ||
            frame.bytes != input_size_) {
            throw std::invalid_argument("inference input does not match configured contiguous device NV21 dimensions");
        }
        check_acl(aclrtMemcpy(input_device_, input_size_, frame.nv21_device, frame.bytes,
                              ACL_MEMCPY_DEVICE_TO_DEVICE), "aclrtMemcpy(NV21)");
        check_acl(aclmdlExecute(model_id_, input_dataset_, output_dataset_), "aclmdlExecute");
        output.resize(output_size_ / sizeof(float));
        check_acl(aclrtMemcpy(output.data(), output_size_, output_device_, output_size_,
                              ACL_MEMCPY_DEVICE_TO_HOST), "aclrtMemcpy(output)");
    }

    int channels() const noexcept { return channels_; }
    int candidates() const noexcept { return candidates_; }
    bool channels_first() const noexcept { return channels_first_; }

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
        input_dataset_ = nullptr;
        output_dataset_ = nullptr;
        input_buffer_ = nullptr;
        output_buffer_ = nullptr;
        input_device_ = nullptr;
        output_device_ = nullptr;
        descriptor_ = nullptr;
        loaded_ = false;
        device_set_ = false;
        initialized_ = false;
    }

    int device_ = 0;
    int input_width_ = 0;
    int input_height_ = 0;
    std::string pixel_format_;
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

class ModelClassroomBehaviorDetection final : public IAIModel {
public:
    void initialize(const ModelProfile& profile, int npu_device_id, const std::string& model_path) override {
        if (profile.model_id != name() || profile.backend != "ascendcl" ||
            profile.decoder != "yolo_v11" || profile.model_family != "yolo11" ||
            profile.preprocessing != "static_aipp" || profile.pixel_format != "nv21") {
            throw std::invalid_argument("classroom behavior detection plugin received an incompatible model profile");
        }
        if (profile.input_width < 1 || profile.input_height < 1 || profile.classes.empty()) {
            throw std::invalid_argument("YOLO11 plugin requires configured input dimensions and class mapping");
        }
        profile_ = profile;
        model_ = std::make_unique<AscendModel>(model_path, npu_device_id, profile_);
        if (model_->channels() != static_cast<int>(profile_.classes.size()) + 4 &&
            model_->channels() != static_cast<int>(profile_.classes.size()) + 5) {
            model_.reset();
            throw std::runtime_error("YOLO output channel count does not match the profile class mapping");
        }
    }

    void infer(const InferenceFrame& frame, double confidence_threshold,
               double nms_threshold, std::vector<Detection>& output) override {
        if (model_ == nullptr) throw std::runtime_error("YOLO11 model is not initialized");
        if (!std::isfinite(confidence_threshold) || confidence_threshold < 0.0 || confidence_threshold > 1.0 ||
            !std::isfinite(nms_threshold) || nms_threshold < 0.0 || nms_threshold > 1.0) {
            throw std::invalid_argument("inference thresholds must be within [0,1]");
        }
        std::vector<float> tensor;
        model_->infer(frame, tensor);
        const int class_offset = model_->channels() == static_cast<int>(profile_.classes.size()) + 5 ? 5 : 4;
        auto at = [&](int channel, int candidate) -> float {
            return model_->channels_first()
                ? tensor[static_cast<size_t>(channel) * model_->candidates() + candidate]
                : tensor[static_cast<size_t>(candidate) * model_->channels() + channel];
        };
        std::vector<std::vector<cv::Rect>> boxes(profile_.classes.size());
        std::vector<std::vector<float>> scores(profile_.classes.size());
        for (int index = 0; index < model_->candidates(); ++index) {
            const float objectness = class_offset == 5 ? at(4, index) : 1.0F;
            int best_class = -1;
            float best_score = 0.0F;
            for (int class_id = 0; class_id < static_cast<int>(profile_.classes.size()); ++class_id) {
                const float score = objectness * at(class_offset + class_id, index);
                if (score > best_score) { best_score = score; best_class = class_id; }
            }
            if (best_class < 0 || best_score < confidence_threshold) continue;
            const float scale = frame.letterbox_scale;
            if (!std::isfinite(scale) || scale <= 0.0F || frame.source_width <= 0 || frame.source_height <= 0 ||
                frame.target_width <= 0 || frame.target_height <= 0) {
                throw std::invalid_argument("source/output frame dimensions or letterbox scale are invalid");
            }
            const float cx = at(0, index);
            const float cy = at(1, index);
            const float width = at(2, index);
            const float height = at(3, index);
            const int x0 = static_cast<int>(std::floor((cx - width * 0.5F - frame.letterbox_left) / scale * frame.target_width / frame.source_width));
            const int y0 = static_cast<int>(std::floor((cy - height * 0.5F - frame.letterbox_top) / scale * frame.target_height / frame.source_height));
            const int x1 = static_cast<int>(std::ceil((cx + width * 0.5F - frame.letterbox_left) / scale * frame.target_width / frame.source_width));
            const int y1 = static_cast<int>(std::ceil((cy + height * 0.5F - frame.letterbox_top) / scale * frame.target_height / frame.source_height));
            const int left = std::clamp(x0, 0, frame.target_width - 1);
            const int top = std::clamp(y0, 0, frame.target_height - 1);
            const int right = std::clamp(x1, left + 1, frame.target_width);
            const int bottom = std::clamp(y1, top + 1, frame.target_height);
            boxes[static_cast<size_t>(best_class)].emplace_back(left, top, right - left, bottom - top);
            scores[static_cast<size_t>(best_class)].push_back(best_score);
        }
        output.clear();
        for (int class_id = 0; class_id < static_cast<int>(profile_.classes.size()); ++class_id) {
            std::vector<int> kept;
            cv::dnn::NMSBoxes(boxes[static_cast<size_t>(class_id)], scores[static_cast<size_t>(class_id)],
                              static_cast<float>(confidence_threshold), static_cast<float>(nms_threshold), kept);
            for (int index : kept) {
                if (output.size() >= static_cast<size_t>(profile_.max_detections)) return;
                const auto& box = boxes[static_cast<size_t>(class_id)][static_cast<size_t>(index)];
                output.push_back(Detection{class_id, profile_.classes[static_cast<size_t>(class_id)],
                                           scores[static_cast<size_t>(class_id)][static_cast<size_t>(index)],
                                           box.x, box.y, box.width, box.height});
            }
        }
        std::sort(output.begin(), output.end(), [](const Detection& left, const Detection& right) {
            return left.confidence > right.confidence;
        });
    }

    void deinitialize() noexcept override { model_.reset(); }
    const char* name() const noexcept override { return "model_classroom_behavior_detection"; }

private:
    ModelProfile profile_;
    std::unique_ptr<AscendModel> model_;
};

}  // namespace
}  // namespace yunshao

extern "C" yunshao::IAIModel* CreateAIModel() { return new yunshao::ModelClassroomBehaviorDetection(); }
extern "C" void DestroyAIModel(yunshao::IAIModel* model) { delete model; }
