<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, shallowRef } from "vue";
import { ElButton, ElDialog, ElMessage } from "element-plus";
import UiIcon from "../../shared/UiIcon.vue";
import { request } from "../../shared/api";
import { errorMessage } from "../identity/store";
import "./avatar.css";
const props = defineProps<{
  avatar?: { id: string; url: string } | null;
  disabled?: boolean;
  founder?: boolean;
  label?: string;
}>();
const value = defineModel<string | null>();
const emit = defineEmits<{ processing: [value: boolean] }>();
interface Draft {
  id: string;
  preview_url: string;
}
const staged = ref<Draft>();
const input = ref<HTMLInputElement>();
const image = shallowRef<HTMLImageElement>();
const dialog = ref(false);
const closing = ref(false);
const reading = ref(false);
const uploading = ref(false);
const cropError = ref("");
const canvas = ref<HTMLCanvasElement>();
const largePreview = ref<HTMLCanvasElement>();
const smallPreview = ref<HTMLCanvasElement>();
const stage = ref<HTMLDivElement>();
const zoom = ref(1),
  angle = ref(0),
  x = ref(0),
  y = ref(0);
const dragging = ref(false);
let drag:
  { id: number; x: number; y: number; left: number; top: number } | undefined;
let url = "",
  generation = 0,
  disposed = false;
const preview = computed(() =>
  value.value === undefined
    ? props.avatar?.url
    : value.value === null
      ? null
      : staged.value?.preview_url,
);
const pending = computed(
  () => reading.value || uploading.value || dialog.value || closing.value,
);
function notifyProcessing() {
  emit("processing", pending.value);
}
async function discard(draft?: Draft) {
  if (draft)
    try {
      await request(`/avatar-uploads/${draft.id}`, { method: "DELETE" });
    } catch {
      /* Expired or bound drafts are never removed by this endpoint. */
    }
}
function saved() {
  staged.value = undefined;
  value.value = undefined;
}
function reset() {
  const old = staged.value;
  staged.value = undefined;
  value.value = undefined;
  void discard(old);
}
defineExpose({ saved, reset });
function select() {
  if (props.disabled || reading.value || uploading.value || closing.value)
    return;
  input.value?.click();
}
function closeCrop() {
  if (uploading.value) return;
  closing.value = true;
  dialog.value = false;
  cropError.value = "";
}
function cropClosed() {
  closing.value = false;
  if (url) URL.revokeObjectURL(url);
  url = "";
  image.value = undefined;
  notifyProcessing();
}
async function choose(event: Event) {
  const field = event.target as HTMLInputElement;
  const file = field.files?.[0];
  field.value = "";
  if (props.disabled || disposed || closing.value || uploading.value) return;
  if (!file) return;
  if (!["image/jpeg", "image/png", "image/webp"].includes(file.type)) {
    ElMessage.error("请选择 JPG、PNG 或 WebP 图片。");
    return;
  }
  if (file.size > 2 * 1024 * 1024) {
    ElMessage.error("头像图片不能超过 2 MB。");
    return;
  }
  const token = ++generation;
  reading.value = true;
  notifyProcessing();
  const selected = URL.createObjectURL(file);
  try {
    const source = new Image();
    source.src = selected;
    await source.decode();
    if (
      !source.naturalWidth ||
      !source.naturalHeight ||
      source.naturalWidth > 4096 ||
      source.naturalHeight > 4096
    )
      throw new Error("dimensions");
    if (disposed || token !== generation) {
      URL.revokeObjectURL(selected);
      return;
    }
    if (url) URL.revokeObjectURL(url);
    url = selected;
    image.value = source;
    zoom.value = 1;
    angle.value = 0;
    x.value = 0;
    y.value = 0;
    cropError.value = "";
    dialog.value = true;
    await nextTick();
    draw();
  } catch {
    URL.revokeObjectURL(selected);
    if (!disposed && token === generation)
      ElMessage.error("图片无法读取或尺寸超过 4096 × 4096，请换一张图片。");
  } finally {
    if (token === generation) {
      reading.value = false;
      notifyProcessing();
    }
  }
}
function draw() {
  const source = image.value,
    output = canvas.value;
  if (!source || !output) return;
  const sideways = angle.value % 180 !== 0;
  const width = sideways ? source.naturalHeight : source.naturalWidth;
  const height = sideways ? source.naturalWidth : source.naturalHeight;
  const scale = Math.max(320 / width, 320 / height) * zoom.value;
  const bx = Math.max(0, (width * scale - 320) / 2),
    by = Math.max(0, (height * scale - 320) / 2);
  x.value = Math.max(-bx, Math.min(bx, x.value));
  y.value = Math.max(-by, Math.min(by, y.value));
  const context = output.getContext("2d")!;
  context.setTransform(1, 0, 0, 1, 0, 0);
  context.clearRect(0, 0, 640, 640);
  context.setTransform(2, 0, 0, 2, 0, 0);
  context.translate(160 + x.value, 160 + y.value);
  context.rotate((angle.value * Math.PI) / 180);
  context.imageSmoothingQuality = "high";
  context.drawImage(
    source,
    (-source.naturalWidth * scale) / 2,
    (-source.naturalHeight * scale) / 2,
    source.naturalWidth * scale,
    source.naturalHeight * scale,
  );
  for (const target of [largePreview.value, smallPreview.value])
    if (target) {
      const ctx = target.getContext("2d")!;
      ctx.clearRect(0, 0, target.width, target.height);
      ctx.drawImage(output, 0, 0, target.width, target.height);
    }
}
function setZoom(v: number) {
  const next = Math.max(1, Math.min(4, v));
  const ratio = next / zoom.value;
  x.value *= ratio;
  y.value *= ratio;
  zoom.value = next;
  draw();
}
function rotate() {
  angle.value = (angle.value + 90) % 360;
  x.value = y.value = 0;
  draw();
}
function resetCrop() {
  zoom.value = 1;
  angle.value = x.value = y.value = 0;
  draw();
}
function pointerDown(e: PointerEvent) {
  if (e.button !== 0 || uploading.value) return;
  stage.value?.focus();
  stage.value?.setPointerCapture(e.pointerId);
  drag = {
    id: e.pointerId,
    x: e.clientX,
    y: e.clientY,
    left: x.value,
    top: y.value,
  };
  dragging.value = true;
  e.preventDefault();
}
function pointerMove(e: PointerEvent) {
  if (!drag || drag.id !== e.pointerId) return;
  const ratio = 320 / stage.value!.getBoundingClientRect().width;
  x.value = drag.left + (e.clientX - drag.x) * ratio;
  y.value = drag.top + (e.clientY - drag.y) * ratio;
  draw();
}
function pointerEnd() {
  drag = undefined;
  dragging.value = false;
}
function keyMove(e: KeyboardEvent) {
  if (
    !["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(e.key) ||
    uploading.value
  )
    return;
  e.preventDefault();
  const n = e.shiftKey ? 10 : 2;
  if (e.key === "ArrowLeft") x.value -= n;
  if (e.key === "ArrowRight") x.value += n;
  if (e.key === "ArrowUp") y.value -= n;
  if (e.key === "ArrowDown") y.value += n;
  draw();
}
async function apply() {
  if (uploading.value || !canvas.value) return;
  uploading.value = true;
  cropError.value = "";
  notifyProcessing();
  try {
    const output = document.createElement("canvas");
    output.width = output.height = 256;
    output.getContext("2d")!.drawImage(canvas.value, 0, 0, 256, 256);
    const blob = await new Promise<Blob>((resolve, reject) =>
      output.toBlob(
        (v) => (v ? resolve(v) : reject(new Error("encoding"))),
        "image/png",
      ),
    );
    const body = new FormData();
    body.append("file", blob, "avatar.png");
    const response = await request<Draft>("/avatar-uploads", {
      method: "POST",
      body,
    });
    if (disposed) {
      void discard(response.data);
      return;
    }
    const old = staged.value;
    staged.value = response.data;
    value.value = response.data.id;
    void discard(old);
    closing.value = true;
    dialog.value = false;
  } catch (err) {
    cropError.value = errorMessage(err);
  } finally {
    uploading.value = false;
    notifyProcessing();
  }
}
function remove() {
  const old = staged.value;
  staged.value = undefined;
  value.value = null;
  void discard(old);
}
onBeforeUnmount(() => {
  disposed = true;
  generation++;
  if (url) URL.revokeObjectURL(url);
  void discard(staged.value);
  emit("processing", false);
});
</script>
<template>
  <div class="avatar-editor">
    <span class="avatar large" :class="{ founder }"
      ><img v-if="preview" :src="preview" alt="头像预览" /><UiIcon
        v-else
        name="user"
    /></span>
    <div class="avatar-editor-controls">
      <h3>{{ label || "用户头像" }}</h3>
      <div class="flex">
        <ElButton
          :disabled="disabled || pending"
          :loading="reading"
          @click="select"
          ><UiIcon name="plus" />上传头像</ElButton
        ><button
          class="btn link"
          type="button"
          :disabled="disabled || pending || !preview"
          @click="remove"
        >
          移除
        </button>
      </div>
      <p class="field-hint" aria-live="polite">
        {{
          value !== undefined
            ? "头像变更将在保存资料后生效。"
            : "支持 JPG、PNG、WebP，最大 2 MB，可手动裁剪。"
        }}
      </p>
    </div>
    <input
      ref="input"
      type="file"
      accept="image/jpeg,image/png,image/webp"
      :aria-label="label === '个人头像' ? '上传个人头像' : '上传用户头像'"
      hidden
      @change="choose"
    />
  </div>
  <ElDialog
    v-model="dialog"
    class="app-dialog avatar-crop-dialog"
    title="裁剪头像"
    width="650px"
    align-center
    append-to-body
    :close-on-click-modal="false"
    :close-on-press-escape="!uploading"
    :show-close="!uploading"
    @closed="cropClosed"
    @close="
      closing = true;
      notifyProcessing();
    "
    @opened="
      draw();
      stage?.focus();
    "
  >
    <template #header="{ titleId }"
      ><h2 :id="titleId">裁剪头像</h2>
      <p class="crop-subtitle">
        拖动图片调整位置，选择你想展示的部分。
      </p></template
    >
    <div class="crop-body">
      <div class="crop-workspace">
        <div
          ref="stage"
          class="crop-stage"
          :class="{ dragging }"
          tabindex="0"
          aria-label="头像裁剪区域，可拖动图片或使用方向键微调"
          @pointerdown="pointerDown"
          @pointermove="pointerMove"
          @pointerup="pointerEnd"
          @pointercancel="pointerEnd"
          @lostpointercapture="pointerEnd"
          @wheel.prevent="
            !uploading && setZoom(zoom + ($event.deltaY < 0 ? 0.1 : -0.1))
          "
          @keydown="keyMove"
        >
          <canvas ref="canvas" width="640" height="640" />
          <div class="crop-grid" aria-hidden="true" />
          <span
            v-for="corner in [
              'top-left',
              'top-right',
              'bottom-left',
              'bottom-right',
            ]"
            :key="corner"
            class="crop-corner"
            :class="corner"
          />
        </div>
        <p class="crop-drag-hint"><UiIcon name="move" />拖动图片调整位置</p>
        <div class="crop-zoom">
          <button
            type="button"
            class="icon-btn"
            aria-label="缩小头像"
            :disabled="uploading"
            @click="setZoom(zoom - 0.1)"
          >
            <UiIcon name="minus" /></button
          ><input
            :value="zoom"
            type="range"
            min="1"
            max="4"
            step="0.01"
            aria-label="头像缩放"
            :disabled="uploading"
            @input="setZoom(Number(($event.target as HTMLInputElement).value))"
          /><button
            type="button"
            class="icon-btn"
            aria-label="放大头像"
            :disabled="uploading"
            @click="setZoom(zoom + 0.1)"
          >
            <UiIcon name="plus" /></button
          ><output>{{ Math.round(zoom * 100) }}%</output>
        </div>
        <div class="crop-tools">
          <ElButton :disabled="uploading" @click="rotate"
            ><UiIcon name="rotate" />旋转 90°</ElButton
          ><ElButton :disabled="uploading" @click="resetCrop"
            ><UiIcon name="reset" />重置</ElButton
          >
        </div>
      </div>
      <aside class="crop-previews">
        <h3>效果预览</h3>
        <div class="crop-preview-item">
          <canvas ref="largePreview" width="128" height="128" /><span
            >个人资料</span
          >
        </div>
        <div class="crop-preview-item compact">
          <canvas ref="smallPreview" width="64" height="64" /><span
            >导航与列表</span
          >
        </div>
        <p>固定 1:1 比例<br />保存为 256 × 256</p>
        <button
          class="btn link"
          type="button"
          :disabled="uploading"
          @click="select"
        >
          重新选择图片
        </button>
      </aside>
    </div>
    <div v-if="cropError" class="crop-error error-banner" role="alert">
      {{ cropError }}
    </div>
    <template #footer
      ><div class="crop-footer">
        <ElButton :disabled="uploading" @click="closeCrop">取消</ElButton
        ><ElButton
          type="primary"
          :loading="uploading"
          :disabled="uploading"
          @click="apply"
          ><UiIcon name="check" />使用此头像</ElButton
        >
      </div></template
    >
  </ElDialog>
</template>
