<script setup lang="ts">
import type { NScrollbar } from 'naive-ui';
import { VueMarkdownItProvider } from 'vue-markdown-shiki';
import ChatMessage from '../chat/modules/chat-message.vue';

defineOptions({
  name: 'ChatHistory'
});

const scrollbarRef = ref<InstanceType<typeof NScrollbar>>();

const conversations = ref<Api.Chat.ConversationHistory[]>([]);
const loading = ref(false);
const selectedId = ref('');

const store = useAuthStore();

const selectedConversation = computed(() => conversations.value.find(c => c.conversationId === selectedId.value));

watch([() => selectedId.value, () => selectedConversation.value?.messages], scrollToBottom, {
  deep: true
});

function scrollToBottom() {
  setTimeout(() => {
    scrollbarRef.value?.scrollBy({
      top: 999999999999999,
      behavior: 'auto'
    });
  }, 100);
}

const range = ref<[number, number]>([dayjs().subtract(7, 'day').valueOf(), dayjs().add(1, 'day').valueOf()]);
const userId = ref<number>(store.userInfo.id);

const params = computed(() => {
  return {
    userid: userId.value,
    archived: true,
    start_date: dayjs(range.value[0]).format('YYYY-MM-DD'),
    end_date: dayjs(range.value[1]).format('YYYY-MM-DD')
  };
});

function formatTime(ts: string) {
  return dayjs(ts).format('MM-DD HH:mm');
}

function displayName(conv: Api.Chat.ConversationHistory) {
  return conv.nickname || conv.username;
}

watchEffect(() => {
  getList();
});

async function getList() {
  if (!params.value.userid) return;
  loading.value = true;
  const { error, data } = await request<Api.Chat.ConversationHistory[]>({
    url: 'admin/conversation',
    params: params.value
  });
  if (!error) {
    conversations.value = data ?? [];
    if (!conversations.value.some(c => c.conversationId === selectedId.value)) {
      selectedId.value = conversations.value[0]?.conversationId ?? '';
    }
    scrollToBottom();
  }
  loading.value = false;
}

function handleDelete(conv: Api.Chat.ConversationHistory) {
  window.$dialog?.warning({
    title: '删除会话',
    content: `确定删除用户 ${conv.nickname || conv.username} 的会话「${conv.title}」吗？删除后不可恢复。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      const { error } = await request({
        url: `admin/conversation/${userId.value}/${conv.conversationId}`,
        method: 'DELETE'
      });
      if (!error) {
        window.$message?.success('会话已删除');
        await getList();
      } else {
        window.$message?.error('删除失败，请稍后重试');
      }
    }
  });
}
</script>

<template>
  <div class="h-full flex gap-4">
    <Teleport defer to="#header-extra">
      <div class="px-10">
        <NForm :model="params" label-placement="left" :show-feedback="false" inline>
          <NFormItem label="用户">
            <TheSelect
              v-model:value="userId"
              url="admin/users/list"
              :params="{ page: 1, size: 999, orgTag: store.userInfo.primaryOrg }"
              key-field="content"
              value-field="userId"
              label-field="username"
              :label-render="(opt: any) => opt.nickname || opt.username"
              class="clear w-200px!"
              :clearable="false"
            />
          </NFormItem>
          <NFormItem label="时间">
            <NDatePicker v-model:value="range" type="daterange" class="clear" />
          </NFormItem>
        </NForm>
      </div>
    </Teleport>

    <!-- 会话侧栏 -->
    <div class="w-240px flex flex-col gap-2 bg-#fff p-3 card-wrapper dark:bg-#1c1c1c">
      <div class="flex items-center justify-between px-1">
        <span class="text-13px font-bold">会话归档</span>
        <span class="text-11px color-gray-400">{{ conversations.length }} 个会话</span>
      </div>
      <NScrollbar class="flex-auto">
        <NSpin :show="loading">
          <div class="flex flex-col gap-1">
            <div
              v-for="conv in conversations"
              :key="conv.conversationId"
              class="group flex cursor-pointer items-center justify-between rounded-[10.67px] py-1.5 pl-2 pr-12px hover:bg-#0001 dark:hover:bg-#fff1"
              :class="{ 'bg-primary/10!': selectedId === conv.conversationId }"
              @click="selectedId = conv.conversationId"
            >
              <span class="flex-1 truncate text-13px">{{ conv.title }}</span>
              <span class="ml-2 text-11px color-gray-400">{{ formatTime(conv.updatedAt) }}</span>
              <span
                class="ml-1 flex items-center text-14px color-gray-400 opacity-0 transition-opacity hover:color-red-500 group-hover:opacity-100"
                @click.stop="handleDelete(conv)"
              >
                <icon-ic-round-delete />
              </span>
            </div>
            <div v-if="!conversations.length" class="py-6 text-center text-12px color-gray-400">暂无归档会话</div>
          </div>
        </NSpin>
      </NScrollbar>
    </div>

    <!-- 会话内容区 -->
    <div class="min-w-0 flex flex-col flex-1 gap-2 bg-#fff p-4 card-wrapper dark:bg-#1c1c1c">
      <template v-if="selectedConversation">
        <header class="flex items-center justify-between gap-4 pb-2">
          <span class="truncate text-14px font-bold">{{ selectedConversation.title }}</span>
          <span class="shrink-0 text-12px color-gray-400">
            {{ displayName(selectedConversation) }} · {{ formatTime(selectedConversation.updatedAt) }} ·
            {{ selectedConversation.messages.length }} 条消息
          </span>
        </header>
        <NScrollbar ref="scrollbarRef" class="flex-auto" content-style="padding-right: 16px">
          <div class="w-full">
            <VueMarkdownItProvider>
              <ChatMessage
                v-for="(item, index) in selectedConversation.messages"
                :key="index"
                :msg="item"
                :username="displayName(selectedConversation)"
              />
            </VueMarkdownItProvider>
          </div>
        </NScrollbar>
      </template>
      <div v-else class="flex flex-1 items-center justify-center">
        <NEmpty v-if="!loading" description="请选择左侧会话查看记录" />
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss"></style>
