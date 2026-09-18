<script setup lang="ts">
import { NScrollbar } from 'naive-ui';
import { VueMarkdownItProvider } from 'vue-markdown-shiki';
import ChatMessage from './chat-message.vue';

defineOptions({
  name: 'ChatList'
});

const chatStore = useChatStore();
const { list } = storeToRefs(chatStore);

const scrollbarRef = ref<InstanceType<typeof NScrollbar>>();

watch(() => [...list.value], scrollToBottom);

function scrollToBottom() {
  setTimeout(() => {
    scrollbarRef.value?.scrollBy({
      top: 999999999999999,
      behavior: 'auto'
    });
  }, 100);
}

onMounted(() => {
  chatStore.scrollToBottom = scrollToBottom;
});
</script>

<template>
  <div class="min-h-0 flex flex-col flex-1">
    <NScrollbar ref="scrollbarRef" class="flex-auto" content-style="padding-right: 16px">
      <Suspense>
        <VueMarkdownItProvider>
          <div v-if="list.length === 0" class="py-10 text-center text-14px color-gray-400">开始和 Zebra RAG 对话吧</div>
          <ChatMessage v-for="(item, index) in list" :key="index" :msg="item" />
        </VueMarkdownItProvider>
      </Suspense>
    </NScrollbar>
  </div>
</template>

<style scoped lang="scss"></style>
