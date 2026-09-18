import { useWebSocket } from '@vueuse/core';

export const useChatStore = defineStore(SetupStoreId.Chat, () => {
  const conversationId = ref<string>('');
  const input = ref<Api.Chat.Input>({ message: '' });

  // 当前会话消息
  const list = ref<Api.Chat.Message[]>([]);
  // 会话列表（多会话）
  const conversations = ref<Api.Chat.Conversation[]>([]);

  // 短时 WS 会话令牌（替代把长期 JWT 放在 URL 中，降低泄漏风险）
  const wsToken = ref<string>('');

  async function ensureWsToken() {
    if (wsToken.value) return true;
    const { error, data } = await request<Api.Chat.Token>({
      url: 'chat/websocket-token',
      baseURL: '/proxy-api'
    });
    if (error || !data?.wsToken) return false;
    wsToken.value = data.wsToken;
    return true;
  }

  const wsUrl = computed(() => (wsToken.value ? `/proxy-ws/chat/${wsToken.value}` : ''));

  const {
    status: wsStatus,
    data: wsData,
    send: wsSend,
    open: wsOpen,
    close: wsClose
  } = useWebSocket(wsUrl, {
    autoReconnect: true,
    immediate: false
  });

  // 建立连接前先获取一次性 WS 令牌，并拉取会话列表
  async function connect() {
    if (await ensureWsToken()) {
      wsOpen();
      await loadConversations();
    }
  }

  async function loadConversations() {
    const { error, data } = await request<Api.Chat.Conversation[]>({
      url: 'users/conversation',
      baseURL: '/proxy-api'
    });
    if (!error && data) {
      conversations.value = data;
    }
  }

  async function selectConversation(id: string) {
    conversationId.value = id;
    const { error, data } = await request<Api.Chat.Message[]>({
      url: `users/conversation/${id}/messages`,
      baseURL: '/proxy-api'
    });
    if (!error) {
      list.value = data ?? [];
    }
  }

  // 删除会话：若删除的是当前会话则回到新会话，随后刷新列表
  async function deleteConversation(id: string) {
    const { error } = await request({
      url: `users/conversation/${id}`,
      method: 'DELETE',
      baseURL: '/proxy-api'
    });
    if (!error) {
      if (conversationId.value === id) newConversation();
      await loadConversations();
    }
    return !error;
  }

  // 归档会话：从「会话助手」移出并进入「会话归档」；若归档的是当前会话则回到新会话
  async function archiveConversation(id: string) {
    const { error } = await request({
      url: `users/conversation/${id}/archive`,
      method: 'POST',
      baseURL: '/proxy-api'
    });
    if (!error) {
      if (conversationId.value === id) newConversation();
      await loadConversations();
    }
    return !error;
  }

  function newConversation() {
    conversationId.value = '';
    list.value = [];
  }

  // 发送 JSON 消息：{"conversationId":"...","query":"..."}；conversationId 为空则服务端新建会话
  function sendQuery(text: string) {
    wsSend(JSON.stringify({ conversationId: conversationId.value, query: text }));
  }

  const scrollToBottom = ref<null | (() => void)>(null);

  return {
    input,
    conversationId,
    list,
    conversations,
    wsToken,
    wsStatus,
    wsData,
    wsSend,
    wsOpen,
    wsClose,
    connect,
    loadConversations,
    selectConversation,
    deleteConversation,
    archiveConversation,
    newConversation,
    sendQuery,
    scrollToBottom
  };
});
