<script setup lang="tsx">
import type { VNodeChild } from 'vue';
import type { UploadFileInfo } from 'naive-ui';
import { NButton, NModal, NPopconfirm, NPopover, NProgress, NTag, NUpload } from 'naive-ui';
import { uploadAccept } from '@/constants/common';
import { fakePaginationRequest } from '@/service/request';
import { UploadStatus } from '@/enum';
import SvgIcon from '@/components/custom/svg-icon.vue';
import FilePreview from '@/components/custom/file-preview.vue';
import UploadDialog from './modules/upload-dialog.vue';
import SearchDialog from './modules/search-dialog.vue';

const appStore = useAppStore();

// 文件预览相关状态
const previewVisible = ref(false);
const previewFileName = ref('');
const previewFileMd5 = ref('');

function apiFn() {
  return fakePaginationRequest<Api.KnowledgeBase.List>({ url: '/documents/accessible' });
}

function renderIcon(fileName: string) {
  const ext = getFileExt(fileName);
  if (ext) {
    if (uploadAccept.split(',').includes(`.${ext}`)) return <SvgIcon localIcon={ext} class="mx-4 text-icon" />;
    return <SvgIcon localIcon="dflt" class="mx-4 text-icon" />;
  }
  return null;
}

// 获取文件类型标签（PDF / TXT / DOCX 等）
function renderFileType(fileName: string) {
  const index = fileName.lastIndexOf('.');
  if (index <= 0) return '其他';
  return fileName.slice(index + 1).toUpperCase();
}

// 处理文件预览
function handleFilePreview(fileName: string, fileMd5?: string) {
  previewFileName.value = fileName;
  previewFileMd5.value = fileMd5 || '';
  previewVisible.value = true;
}

// 关闭文件预览
function closeFilePreview() {
  previewVisible.value = false;
  previewFileName.value = '';
  previewFileMd5.value = '';
}

const { columns, columnChecks, data, getData, loading } = useTable({
  apiFn,
  immediate: false,
  columns: () => [
    {
      key: 'fileName',
      title: '文件名',
      minWidth: 260,
      render: row => (
        <div class="min-w-0 flex items-center">
          {renderIcon(row.fileName)}
          <span
            class="block min-w-0 flex-1 cursor-pointer truncate transition-colors hover:text-primary"
            onClick={() => handleFilePreview(row.fileName)}
          >
            {row.fileName}
          </span>
        </div>
      )
    },
    {
      key: 'fileType',
      title: '文件类型',
      width: 100,
      render: row => (
        <NTag bordered={false} size="small">
          {renderFileType(row.fileName)}
        </NTag>
      )
    },
    {
      key: 'totalSize',
      title: '文件大小',
      width: 100,
      render: row => fileSize(row.totalSize)
    },
    {
      key: 'status',
      title: '上传状态',
      width: 100,
      render: row => renderStatus(row.status, row.progress)
    },
    {
      key: 'fileMd5',
      title: 'MD5',
      width: 150,
      render: row =>
        renderCopyPopover(
          <span class="block truncate font-mono">{row.fileMd5}</span>,
          row.fileMd5,
          <div class="break-all font-mono">{row.fileMd5}</div>
        )
    },
    {
      key: 'estimatedTokens',
      title: '预估向量化',
      width: 130,
      render: row => renderEstimated(row)
    },
    {
      key: 'actualTokens',
      title: '实际向量化',
      width: 130,
      render: row => renderActual(row)
    },
    {
      key: 'orgTagName',
      title: '组织标签',
      width: 150,
      ellipsis: { tooltip: true, lineClamp: 2 }
    },
    {
      key: 'isPublic',
      title: '是否公开',
      width: 100,
      render: row =>
        row.isPublic ? (
          <NTag bordered={false} type="success">
            公开
          </NTag>
        ) : (
          <NTag bordered={false} type="warning">
            私有
          </NTag>
        )
    },
    {
      key: 'createdAt',
      title: '上传时间',
      width: 100,
      render: row => dayjs(row.createdAt).format('YYYY-MM-DD')
    },
    {
      key: 'operate',
      title: '操作',
      width: 180,
      render: row => (
        <div class="flex gap-4">
          {renderResumeUploadButton(row)}
          <NButton type="primary" ghost size="small" onClick={() => handleFilePreview(row.fileName)}>
            预览
          </NButton>
          <NPopconfirm onPositiveClick={() => handleDelete(row.fileMd5)}>
            {{
              default: () => '确认删除当前文件吗？',
              trigger: () => (
                <NButton type="error" ghost size="small">
                  删除
                </NButton>
              )
            }}
          </NPopconfirm>
        </div>
      )
    }
  ]
});

const store = useKnowledgeBaseStore();
const { tasks } = storeToRefs(store);
onMounted(async () => {
  await getList();
});

/** 异步获取列表函数 该函数主要用于更新或初始化上传任务列表 它首先调用getData函数获取数据，然后根据获取到的数据状态更新任务列表 */
async function getList() {
  // 等待获取最新数据
  await getData();

  if (data.value.length === 0) {
    tasks.value = [];
    return;
  }

  // 遍历获取到的数据，以处理每个项目
  data.value.forEach(item => {
    // 检查项目状态是否为已完成
    if (item.status === UploadStatus.Completed) {
      // 查找任务列表中是否有匹配的文件MD5
      const index = tasks.value.findIndex(task => task.fileMd5 === item.fileMd5);
      // 如果找到匹配项，则更新其状态
      if (index !== -1) {
        tasks.value[index].status = UploadStatus.Completed;
      } else {
        // 如果没有找到匹配项，则将该项目添加到任务列表中
        tasks.value.push(item);
      }
    } else if (!tasks.value.some(task => task.fileMd5 === item.fileMd5)) {
      // 如果项目状态不是已完成，并且任务列表中没有相同的文件MD5，则将该项目的状态设置为中断，并添加到任务列表中
      item.status = UploadStatus.Break;
      tasks.value.push(item);
    }
  });
}

async function handleDelete(fileMd5: string) {
  const index = tasks.value.findIndex(task => task.fileMd5 === fileMd5);

  if (index !== -1) {
    tasks.value[index].requestIds?.forEach(requestId => {
      request.cancelRequest(requestId);
    });
  }

  // 如果文件一个分片也没有上传完成，则直接删除
  if (tasks.value[index].uploadedChunks && tasks.value[index].uploadedChunks.length === 0) {
    tasks.value.splice(index, 1);
    return;
  }

  const { error } = await request({ url: `/documents/${fileMd5}`, method: 'DELETE' });
  if (!error) {
    tasks.value.splice(index, 1);
    window.$message?.success('删除成功');
    await getData();
  }
}

// #region 文件上传
const uploadVisible = ref(false);
function handleUpload() {
  uploadVisible.value = true;
}
// #endregion

// #region 检索知识库
const searchVisible = ref(false);
function handleSearch() {
  searchVisible.value = true;
}
// #endregion

// 渲染上传状态
function renderStatus(status: UploadStatus, percentage: number) {
  if (status === UploadStatus.Completed)
    return (
      <NTag bordered={false} type="success">
        已完成
      </NTag>
    );
  else if (status === UploadStatus.Break)
    return (
      <NTag bordered={false} type="error">
        上传中断
      </NTag>
    );
  return <NProgress percentage={percentage} processing />;
}

// 复制文本到剪贴板
function handleCopy(content: string) {
  navigator.clipboard.writeText(content);
  window.$message?.success('已复制');
}

// 可复制的浮动窗口：hover 显示完整内容，文字末尾附复制图标
function renderCopyPopover(trigger: VNodeChild, copyText: string, content: VNodeChild) {
  return (
    <NPopover trigger="hover" placement="top-start">
      {{
        trigger: () => trigger,
        default: () => (
          <div class="max-w-360px">
            <div class="flex items-start gap-2">
              <div class="min-w-0 flex-1 text-12px">{content}</div>
              <span
                class="mt-0.5 cursor-pointer text-14px color-gray-400 transition-colors hover:color-primary"
                onClick={() => handleCopy(copyText)}
              >
                <icon-ic-round-content-copy />
              </span>
            </div>
          </div>
        )
      }}
    </NPopover>
  );
}

// 渲染预估向量化：基于原文估算的 token 与切片数
function renderEstimated(row: Api.KnowledgeBase.UploadTask) {
  if (!row.vectorizedStatus || row.vectorizedStatus === 'pending') {
    return <span class="text-12px color-gray-400">待处理</span>;
  }
  if (!row.estimatedTokens) {
    return <span class="text-12px color-gray-400">-</span>;
  }
  const tokens = row.estimatedTokens;
  const chunks = row.estimatedChunks ?? 0;
  const cell = (
    <div class="flex flex-col">
      <span class="text-12px">{tokens} Tokens</span>
      <span class="text-12px color-gray-400">{chunks} 个切片</span>
    </div>
  );
  return renderCopyPopover(
    cell,
    `预估向量化：${tokens} Tokens / ${chunks} 个切片`,
    <div class="flex flex-col">
      <span>{tokens} Tokens</span>
      <span>{chunks} 个切片</span>
    </div>
  );
}

// 渲染实际向量化：入库统计 token 与切片数，失败时展示失败原因
function renderActual(row: Api.KnowledgeBase.UploadTask) {
  const status = row.vectorizedStatus;
  if (!status || status === 'pending') {
    return <span class="text-12px color-gray-400">待处理</span>;
  }
  if (status === 'success') {
    const tokens = row.actualTokens ?? 0;
    const chunks = row.actualChunks ?? 0;
    const cell = (
      <div class="flex flex-col">
        <span class="text-12px color-green">{tokens} Tokens</span>
        <span class="text-12px color-gray-400">{chunks} 个切片</span>
      </div>
    );
    return renderCopyPopover(
      cell,
      `实际向量化：${tokens} Tokens / ${chunks} 个切片`,
      <div class="flex flex-col">
        <span>{tokens} Tokens</span>
        <span>{chunks} 个切片</span>
      </div>
    );
  }
  if (status === 'failed') {
    const error = row.vectorizeError || '未知错误';
    return renderCopyPopover(
      <span class="text-12px color-red-500">向量化失败</span>,
      `实际向量化：向量化失败：${error}`,
      <div class="break-all color-red-500">向量化失败：{error}</div>
    );
  }
  return <span class="text-12px color-primary">向量化处理中…</span>;
}

// #region 文件续传
function renderResumeUploadButton(row: Api.KnowledgeBase.UploadTask) {
  if (row.status === UploadStatus.Break) {
    if (row.file)
      return (
        <NButton type="primary" size="small" ghost onClick={() => resumeUpload(row)}>
          续传
        </NButton>
      );
    return (
      <NUpload
        show-file-list={false}
        default-upload={false}
        accept={uploadAccept}
        onBeforeUpload={options => onBeforeUpload(options, row)}
        class="w-fit"
      >
        <NButton type="primary" size="small" ghost>
          续传
        </NButton>
      </NUpload>
    );
  }
  return null;
}

// 任务列表存在文件，直接续传
function resumeUpload(row: Api.KnowledgeBase.UploadTask) {
  row.status = UploadStatus.Pending;
  store.startUpload();
}

async function onBeforeUpload(
  options: { file: UploadFileInfo; fileList: UploadFileInfo[] },
  row: Api.KnowledgeBase.UploadTask
) {
  const md5 = await calculateMD5(options.file.file!);
  if (md5 !== row.fileMd5) {
    window.$message?.error('两次上传的文件不一致');
    return false;
  }
  loading.value = true;
  const { error, data: progress } = await request<Api.KnowledgeBase.Progress>({
    url: '/upload/status',
    params: { file_md5: row.fileMd5 }
  });
  if (!error) {
    row.file = options.file.file!;
    row.status = UploadStatus.Pending;
    row.progress = progress.progress;
    row.uploadedChunks = progress.uploaded;
    store.startUpload();
    loading.value = false;
    return true;
  }
  loading.value = false;
  return false;
}
</script>

<template>
  <div class="min-h-500px flex-col-stretch gap-16px overflow-hidden lt-sm:overflow-auto">
    <NCard title="文件列表" :bordered="false" size="small" class="sm:flex-1-hidden card-wrapper">
      <template #header-extra>
        <TableHeaderOperation v-model:columns="columnChecks" :loading="loading" @add="handleUpload" @refresh="getList">
          <template #prefix>
            <NButton size="small" ghost type="primary" @click="handleSearch">
              <template #icon>
                <icon-ic-round-search class="text-icon" />
              </template>
              检索知识库
            </NButton>
          </template>
        </TableHeaderOperation>
      </template>
      <NDataTable
        striped
        :columns="columns"
        :data="tasks"
        size="small"
        :flex-height="!appStore.isMobile"
        :scroll-x="1500"
        :loading="loading"
        remote
        :row-key="row => row.id"
        :pagination="false"
        class="sm:h-full"
      />
    </NCard>
    <UploadDialog v-model:visible="uploadVisible" />
    <SearchDialog v-model:visible="searchVisible" />

    <!-- 文件预览弹窗 -->
    <NModal
      v-model:show="previewVisible"
      preset="card"
      title="文件预览"
      style="width: 80%; max-width: 1000px; height: 75vh"
    >
      <FilePreview
        :file-name="previewFileName"
        :file-md5="previewFileMd5 || undefined"
        :visible="previewVisible"
        @close="closeFilePreview"
      />
    </NModal>
  </div>
</template>

<style scoped lang="scss">
.file-list-container {
  transition: width 0.3s ease;
}

:deep() {
  .n-progress-icon.n-progress-icon--as-text {
    white-space: nowrap;
  }
}
</style>
