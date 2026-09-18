<script setup lang="ts">
import { ref, watch } from 'vue';
import { NButton, NSpin } from 'naive-ui';
import { request } from '@/service/request';
import { getFileExt } from '@/utils/common';
import SvgIcon from '@/components/custom/svg-icon.vue';

interface Props {
  fileName: string;
  fileMd5?: string;
  visible: boolean;
}

interface Emits {
  (e: 'close'): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

const loading = ref(false);
const downloading = ref(false);
const content = ref('');
const error = ref('');

// 获取文件图标
function getFileIcon(fileName: string) {
  const ext = getFileExt(fileName);
  if (ext) {
    const supportedIcons = ['pdf', 'doc', 'docx', 'txt', 'md', 'jpg', 'jpeg', 'png', 'gif'];
    return supportedIcons.includes(ext.toLowerCase()) ? ext : 'dflt';
  }
  return 'dflt';
}

// 监听文件名变化，加载预览内容
watch(
  () => props.fileName,
  async newFileName => {
    if (newFileName && props.visible) {
      await loadPreviewContent();
    }
  },
  { immediate: true }
);

// 监听可见性变化
watch(
  () => props.visible,
  async visible => {
    if (visible && props.fileName) {
      await loadPreviewContent();
    }
  }
);

// 加载预览内容
async function loadPreviewContent() {
  if (!props.fileName) return;

  loading.value = true;
  error.value = '';
  content.value = '';

  try {
    const { error: requestError, data } = await request<{
      fileName: string;
      content: string;
      fileSize: number;
    }>({
      url: '/documents/preview',
      params: {
        fileName: props.fileName,
        fileMd5: props.fileMd5 || undefined
      }
    });

    if (requestError) {
      error.value = `预览失败：${requestError.message || '未知错误'}`;
    } else if (data) {
      content.value = data.content;
    }
  } catch (err: any) {
    error.value = `预览失败：${err.message || '网络错误'}`;
  } finally {
    loading.value = false;
  }
}

// 下载文件
async function downloadFile() {
  if (!props.fileName) return;

  downloading.value = true;

  try {
    const { error: requestError, data } = await request<{
      fileName: string;
      downloadUrl: string;
      fileSize: number;
    }>({
      url: '/documents/download',
      params: {
        fileName: props.fileName,
        fileMd5: props.fileMd5 || undefined
      }
    });

    if (requestError) {
      window.$message?.error(`下载失败：${requestError.message || '未知错误'}`);
    } else if (data) {
      // 使用预签名URL下载文件
      const link = document.createElement('a');
      link.href = data.downloadUrl;
      link.download = data.fileName;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      window.$message?.success('开始下载文件');
    }
  } catch (err: any) {
    window.$message?.error(`下载失败：${err.message || '网络错误'}`);
  } finally {
    downloading.value = false;
  }
}

// 关闭预览
function closePreview() {
  emit('close');
}
</script>

<template>
  <div class="file-preview-container" style="height: 60vh; display: flex; flex-direction: column">
    <!-- 预览头部 -->
    <div class="preview-header">
      <div class="flex items-center gap-2">
        <SvgIcon :local-icon="getFileIcon(fileName)" class="text-16" />
        <span class="font-medium">{{ fileName }}</span>
      </div>
      <div class="flex items-center gap-2">
        <NButton size="small" :loading="downloading" @click="downloadFile">
          <template #icon>
            <icon-mdi-download />
          </template>
          下载
        </NButton>
        <NButton size="small" @click="closePreview">
          <template #icon>
            <icon-mdi-close />
          </template>
        </NButton>
      </div>
    </div>

    <!-- 预览内容 -->
    <div class="preview-content">
      <template v-if="loading">
        <div class="h-full flex items-center justify-center">
          <NSpin size="large" />
        </div>
      </template>
      <template v-else-if="error">
        <div class="h-full flex flex-col items-center justify-center text-gray-500">
          <icon-mdi-alert-circle class="mb-4 text-48" />
          <p>{{ error }}</p>
        </div>
      </template>
      <template v-else-if="content">
        <div class="content-wrapper" style="height: 100%; overflow: auto">
          <pre class="preview-text">{{ content }}</pre>
        </div>
      </template>
      <template v-else>
        <div class="h-full flex items-center justify-center text-gray-400">该文件无可预览的文本内容</div>
      </template>
    </div>
  </div>
</template>

<style scoped lang="scss">
.file-preview-container {
  /* 固定高度，避免父级(NCard/NModal)高度未透传导致内容区塌陷 */
  @apply flex flex-col bg-white border-l border-gray-200;
  height: 60vh;
  color: #1c1c1c; /* 强制深色文字，避免暗色主题下白字白底不可见 */
  background: #fff;

  .preview-header {
    @apply flex items-center justify-between p-4 border-b border-gray-200 bg-gray-50;
  }

  .preview-content {
    @apply flex-1 overflow-hidden;
    min-height: 0; /* flex 子项允许收缩，配合 h-full overflow-auto 实现滚动 */

    .content-wrapper {
      @apply h-full overflow-auto p-4;
    }

    .preview-text {
      @apply text-sm font-mono whitespace-pre-wrap break-words;
      font-family: 'Monaco', 'Menlo', 'Ubuntu Mono', monospace;
      line-height: 1.5;
      margin: 0;
      color: #1c1c1c; /* 深色文字兜底 */
    }
  }
}
</style>
