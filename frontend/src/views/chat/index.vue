<script setup lang="ts">
import ChatList from './modules/chat-list.vue';
import InputBox from './modules/input-box.vue';

const chatStore = useChatStore();

onMounted(() => {
  // 进入会话页即获取短时 WS 令牌并建立连接、加载会话列表
  chatStore.connect();
});

function formatTime(ts: string) {
  return dayjs(ts).format('MM-DD HH:mm');
}

function handleArchive(conv: Api.Chat.Conversation) {
  window.$dialog?.info({
    title: '归档会话',
    content: `确定归档会话「${conv.title}」吗？归档后可在「会话归档」中查看。`,
    positiveText: '归档',
    negativeText: '取消',
    onPositiveClick: async () => {
      const ok = await chatStore.archiveConversation(conv.conversationId);
      if (ok) {
        window.$message?.success('会话已归档');
      } else {
        window.$message?.error('归档失败，请稍后重试');
      }
    }
  });
}
</script>

<template>
  <div class="h-full flex gap-4">
    <!-- 会话侧栏 -->
    <div class="w-240px flex flex-col gap-2 bg-#fff p-3 card-wrapper dark:bg-#1c1c1c">
      <NButton type="primary" block @click="chatStore.newConversation()">
        <template #icon><icon-mynaui:plus /></template>
        新会话
      </NButton>
      <NScrollbar class="flex-auto">
        <div class="flex flex-col gap-1">
          <div
            v-for="c in chatStore.conversations"
            :key="c.conversationId"
            class="group flex cursor-pointer items-center justify-between rounded-[10.67px] py-1.5 pl-2 pr-12px hover:bg-#0001 dark:hover:bg-#fff1"
            :class="{ 'bg-primary/10!': chatStore.conversationId === c.conversationId }"
            @click="chatStore.selectConversation(c.conversationId)"
          >
            <span class="flex-1 truncate text-13px">{{ c.title }}</span>
            <span class="ml-2 text-11px color-gray-400">{{ formatTime(c.updatedAt) }}</span>
            <span
              class="ml-1 flex items-center text-14px color-gray-400 opacity-0 transition-opacity hover:color-primary group-hover:opacity-100"
              @click.stop="handleArchive(c)"
            >
              <icon-ic-round-archive />
            </span>
          </div>
          <div v-if="chatStore.conversations.length === 0" class="py-6 text-center text-12px color-gray-400">
            暂无会话
          </div>
        </div>
      </NScrollbar>
    </div>

    <!-- 会话区 -->
    <div class="min-w-0 flex flex-col flex-1 gap-2 bg-#fff p-4 card-wrapper dark:bg-#1c1c1c">
      <ChatList />
      <InputBox />
    </div>
  </div>
</template>

<style scoped></style>
