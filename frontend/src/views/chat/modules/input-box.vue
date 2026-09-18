<script setup lang="ts">
const chatStore = useChatStore();
const { input, list, wsStatus, wsData } = storeToRefs(chatStore);

const latestMessage = computed(() => {
  return list.value[list.value.length - 1] ?? {};
});

const isSending = computed(() => {
  return (
    latestMessage.value?.role === 'assistant' && ['loading', 'pending'].includes(latestMessage.value?.status || '')
  );
});

const sendable = computed(
  () => (!input.value.message && !isSending) || ['CLOSED', 'CONNECTING'].includes(wsStatus.value)
);

const textareaRef = ref<HTMLTextAreaElement>();

const MIN_HEIGHT = 40;
const MAX_HEIGHT = 300;
const resizing = ref(false);
const startDrag = (e: PointerEvent) => {
  resizing.value = true;
  const startY = e.clientY;
  const startHeight = textareaRef.value?.offsetHeight ?? MIN_HEIGHT;
  const onMove = (ev: PointerEvent) => {
    const next = Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, startHeight - (ev.clientY - startY)));
    if (textareaRef.value) textareaRef.value.style.height = `${next}px`;
  };
  const onUp = () => {
    resizing.value = false;
    document.removeEventListener('pointermove', onMove);
    document.removeEventListener('pointerup', onUp);
  };
  document.addEventListener('pointermove', onMove);
  document.addEventListener('pointerup', onUp);
};

watch(wsData, val => {
  const data = JSON.parse(val);
  const assistant = list.value[list.value.length - 1];

  if (data.type === 'completion' && data.status === 'finished' && assistant.status !== 'error') {
    assistant.status = 'finished';
    // 刷新会话列表；若为新会话则选中最新一条
    chatStore.loadConversations();
    if (!chatStore.conversationId && chatStore.conversations.length > 0) {
      chatStore.selectConversation(chatStore.conversations[0].conversationId);
    }
  }
  if (data.error) assistant.status = 'error';
  else if (data.chunk) {
    assistant.status = 'loading';
    assistant.content += data.chunk;
  }
});

const handleSend = async () => {
  //  判断是否正在发送, 如果发送中，则停止ai继续响应
  if (isSending.value) {
    // 停止令牌即本连接的 wsToken（per-session），不再每次请求 cmdToken
    chatStore.wsSend(JSON.stringify({ type: 'stop', _internal_cmd_token: chatStore.wsToken }));

    list.value[list.value.length - 1].status = 'finished';
    if (!latestMessage.value.content) list.value.pop();
    return;
  }

  // 确保 WebSocket 连接就绪
  await chatStore.connect();

  list.value.push({
    content: input.value.message,
    role: 'user'
  });
  chatStore.sendQuery(input.value.message);
  list.value.push({
    content: '',
    role: 'assistant',
    status: 'pending'
  });
  input.value.message = '';
};

// 手动插入换行符（确保所有浏览器兼容）
const insertNewline = () => {
  const textarea = textareaRef.value;
  if (!textarea) return;
  const start = textarea.selectionStart;
  const end = textarea.selectionEnd;

  // 在光标位置插入换行符
  input.value.message = `${input.value.message.substring(0, start)}\n${input.value.message.substring(end)}`;

  // 更新光标位置（在插入的换行符之后）
  nextTick(() => {
    textarea.selectionStart = start + 1;
    textarea.selectionEnd = start + 1;
    textarea.focus(); // 确保保持焦点
  });
};

// ctrl + enter 换行
// enter 发送
const handShortcut = (e: KeyboardEvent) => {
  if (e.key === 'Enter') {
    e.preventDefault();

    if (!e.shiftKey && !e.ctrlKey) {
      handleSend();
    } else insertNewline();
  }
};
</script>

<template>
  <div class="relative w-full pt-3">
    <div
      class="group absolute top-0 right-0 left-0 flex cursor-ns-resize items-center justify-center select-none"
      :class="resizing ? 'h-4 bg-primary/10' : 'h-1'"
      @pointerdown="startDrag"
    >
      <div class="h-px w-full bg-gray-200 transition-colors group-hover:bg-primary dark:bg-#333" />
    </div>
    <textarea
      ref="textareaRef"
      v-model.trim="input.message"
      placeholder="给 Zebra RAG 发送消息（Enter 发送，Shift+Enter 换行）"
      class="min-h-10 w-full cursor-text resize-none b-none bg-transparent color-#333 caret-[rgb(var(--primary-color))] outline-none dark:color-#f1f1f1"
      @keydown="handShortcut"
    />
    <div class="flex items-center justify-between pt-2">
      <div class="flex items-center text-18px color-gray-500">
        <NText class="text-14px">连接状态：</NText>
        <icon-eos-icons:loading v-if="wsStatus === 'CONNECTING'" class="color-yellow" />
        <icon-fluent:plug-connected-checkmark-20-filled v-else-if="wsStatus === 'OPEN'" class="color-green" />
        <icon-tabler:plug-connected-x v-else class="color-red" />
      </div>
      <NButton :disabled="sendable" strong circle type="primary" @click="handleSend">
        <template #icon>
          <icon-material-symbols:stop-rounded v-if="isSending" />
          <icon-guidance:send v-else />
        </template>
      </NButton>
    </div>
  </div>
</template>

<style scoped></style>
