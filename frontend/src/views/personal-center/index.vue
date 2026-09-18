<script setup lang="ts">
import { fetchChangePassword, fetchUpdateProfile } from '@/service/api';

const router = useRouter();
const { userInfo } = storeToRefs(useAuthStore());
const authStore = useAuthStore();
const chatStore = useChatStore();

const displayName = computed(() => userInfo.value.nickname || userInfo.value.username);

// 组织标签
const tags = ref<Api.OrgTag.Mine>({
  orgTags: [],
  primaryOrg: '',
  orgTagDetails: []
});

const loading = ref(false);
const getOrgTags = async () => {
  loading.value = true;
  const { error, data } = await request<Api.OrgTag.Mine>({
    url: '/users/org-tags'
  });
  if (!error) {
    tags.value = data;
  }
  loading.value = false;
};

// 数据概览统计
const stats = ref<Api.Auth.UserStats>({
  conversationCount: 0,
  archivedConversationCount: 0,
  documentCount: 0,
  accessibleDocumentCount: 0
});
const statsLoading = ref(false);
const getStats = async () => {
  statsLoading.value = true;
  const { error, data } = await request<Api.Auth.UserStats>({
    url: '/users/stats'
  });
  if (!error && data) {
    stats.value = data;
  }
  statsLoading.value = false;
};

// 最近会话
const recentConversations = ref<Api.Chat.Conversation[]>([]);
const getRecentConversations = async () => {
  const { error, data } = await request<Api.Chat.Conversation[]>({
    url: '/users/conversation'
  });
  if (!error && data) {
    recentConversations.value = data.slice(0, 5);
  }
};

async function openConversation(conv: Api.Chat.Conversation) {
  await chatStore.selectConversation(conv.conversationId);
  router.push('/chat');
}

function formatDate(ts?: string) {
  return ts ? dayjs(ts).format('YYYY-MM-DD') : '-';
}

function formatTime(ts: string) {
  return dayjs(ts).format('MM-DD HH:mm');
}

onMounted(() => {
  getOrgTags();
  getStats();
  getRecentConversations();
});

// 设置主标签
const visible = ref(false);
const currentTagId = ref('');
const showModal = (tagId: string) => {
  if (tagId === tags.value.primaryOrg) return;
  visible.value = true;
  currentTagId.value = tagId;
};
const submitLoading = ref(false);
const setPrimaryOrg = async () => {
  submitLoading.value = true;
  const { error } = await request({
    url: '/users/primary-org',
    method: 'PUT',
    data: { primaryOrg: currentTagId.value, userId: userInfo.value.id }
  });
  if (!error) {
    visible.value = false;
    getOrgTags();
  }
  submitLoading.value = false;
};

// 编辑资料
const editVisible = ref(false);
const editForm = reactive({
  nickname: '',
  email: '',
  phone: '',
  bio: ''
});
const editLoading = ref(false);
const openEdit = () => {
  editForm.nickname = userInfo.value.nickname || '';
  editForm.email = userInfo.value.email || '';
  editForm.phone = userInfo.value.phone || '';
  editForm.bio = userInfo.value.bio || '';
  editVisible.value = true;
};
const saveProfile = async () => {
  editLoading.value = true;
  const { error } = await fetchUpdateProfile({ ...editForm });
  if (!error) {
    window.$message?.success('个人资料已更新');
    editVisible.value = false;
    await authStore.getUserInfo();
  }
  editLoading.value = false;
};

// 修改密码
const pwdVisible = ref(false);
const pwdForm = reactive({
  oldPassword: '',
  newPassword: '',
  confirmPassword: ''
});
const pwdLoading = ref(false);
const savePassword = async () => {
  if (!pwdForm.oldPassword || !pwdForm.newPassword) {
    window.$message?.error('请填写原密码与新密码');
    return;
  }
  if (pwdForm.newPassword.length < 6) {
    window.$message?.error('新密码长度至少为 6 位');
    return;
  }
  if (pwdForm.newPassword !== pwdForm.confirmPassword) {
    window.$message?.error('两次输入的新密码不一致');
    return;
  }
  pwdLoading.value = true;
  const { error } = await fetchChangePassword({
    oldPassword: pwdForm.oldPassword,
    newPassword: pwdForm.newPassword
  });
  if (!error) {
    window.$message?.success('密码修改成功，请重新登录');
    pwdVisible.value = false;
    pwdForm.oldPassword = '';
    pwdForm.newPassword = '';
    pwdForm.confirmPassword = '';
  }
  pwdLoading.value = false;
};
</script>

<template>
  <NSpin :show="loading || statsLoading">
    <div class="min-h-500px flex-col-stretch gap-16px overflow-auto lt-sm:overflow-auto">
      <!-- 用户信息 -->
      <NCard :bordered="false" class="card-wrapper">
        <div class="flex items-center gap-4">
          <NAvatar size="large" :src="userInfo.avatar || undefined">
            <icon-solar:user-circle-linear v-if="!userInfo.avatar" class="text-icon-large" />
          </NAvatar>
          <div class="flex flex-col gap-1">
            <div class="flex items-center gap-2 text-16px font-semibold">
              {{ displayName }}
              <NTag v-if="userInfo.role === 'ADMIN'" type="warning" size="small" :bordered="false">管理员</NTag>
              <NTag v-else size="small" type="info" :bordered="false">普通用户</NTag>
            </div>
            <div class="text-12px color-gray-400">@{{ displayName }}</div>
          </div>
          <div class="ml-auto flex items-center gap-2">
            <NButton size="small" @click="openEdit">
              <template #icon>
                <icon-ic-round-edit />
              </template>
              编辑资料
            </NButton>
            <NButton size="small" secondary @click="pwdVisible = true">
              <template #icon>
                <icon-ic-round-lock />
              </template>
              修改密码
            </NButton>
          </div>
        </div>
        <div class="grid grid-cols-1 mt-4 gap-3 text-13px color-gray-500 sm:grid-cols-3">
          <div class="flex items-center gap-2">
            <icon-ic-round-person class="text-16px" />
            用户 ID：{{ userInfo.id }}
          </div>
          <div class="flex items-center gap-2">
            <icon-ic-round-verified class="text-16px" />
            角色：{{ userInfo.role === 'ADMIN' ? '管理员' : '普通用户' }}
          </div>
          <div class="flex items-center gap-2">注册时间：{{ formatDate(userInfo.createdAt) }}</div>
        </div>
        <div v-if="userInfo.bio" class="mt-3 text-13px color-gray-400">{{ userInfo.bio }}</div>
      </NCard>

      <!-- 数据概览 -->
      <NCard :bordered="false" title="数据概览" class="card-wrapper">
        <div class="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <div class="flex items-center gap-3 rounded-8px bg-#f5f5f5 p-4 dark:bg-#2a2a2a">
            <div class="h-10 w-10 flex items-center justify-center rounded-full bg-primary/10 text-22px color-primary">
              <icon-ic-round-chat-bubble-outline />
            </div>
            <div>
              <div class="text-20px font-semibold">{{ stats.conversationCount }}</div>
              <div class="text-12px color-gray-400">会话总数</div>
            </div>
          </div>
          <div class="flex items-center gap-3 rounded-8px bg-#f5f5f5 p-4 dark:bg-#2a2a2a">
            <div class="h-10 w-10 flex items-center justify-center rounded-full bg-primary/10 text-22px color-primary">
              <icon-ic-round-archive />
            </div>
            <div>
              <div class="text-20px font-semibold">{{ stats.archivedConversationCount }}</div>
              <div class="text-12px color-gray-400">归档会话</div>
            </div>
          </div>
          <div class="flex items-center gap-3 rounded-8px bg-#f5f5f5 p-4 dark:bg-#2a2a2a">
            <div class="h-10 w-10 flex items-center justify-center rounded-full bg-primary/10 text-22px color-primary">
              <icon-ic-round-description />
            </div>
            <div>
              <div class="text-20px font-semibold">{{ stats.documentCount }}</div>
              <div class="text-12px color-gray-400">我的文档</div>
            </div>
          </div>
          <div class="flex items-center gap-3 rounded-8px bg-#f5f5f5 p-4 dark:bg-#2a2a2a">
            <div class="h-10 w-10 flex items-center justify-center rounded-full bg-primary/10 text-22px color-primary">
              <icon-ic-round-library-books />
            </div>
            <div>
              <div class="text-20px font-semibold">{{ stats.accessibleDocumentCount }}</div>
              <div class="text-12px color-gray-400">可访问文档</div>
            </div>
          </div>
        </div>
      </NCard>

      <!-- 最近会话 -->
      <NCard :bordered="false" title="最近会话" class="card-wrapper">
        <NEmpty v-if="recentConversations.length === 0" description="暂无会话" />
        <div v-else class="flex flex-col gap-1">
          <div
            v-for="conv in recentConversations"
            :key="conv.conversationId"
            class="flex cursor-pointer items-center gap-2 rounded-6px px-2 py-2 transition-colors hover:bg-#f5f5f5 dark:hover:bg-#2a2a2a"
            @click="openConversation(conv)"
          >
            <icon-ic-round-chat-bubble-outline class="text-16px color-gray-400" />
            <span class="flex-1 truncate text-13px">{{ conv.title }}</span>
            <span class="text-11px color-gray-400">{{ formatTime(conv.updatedAt) }}</span>
            <icon-ic-round-chevron-right class="text-14px color-gray-300" />
          </div>
        </div>
      </NCard>

      <!-- 组织标签 -->
      <NCard
        :bordered="false"
        title="组织标签"
        class="sm:flex-1-hidden card-wrapper"
        :segmented="{ content: true, footer: 'soft' }"
      >
        <NScrollbar class="max-h-60vh">
          <div class="flex flex-wrap gap-4 p-4">
            <div
              v-for="tag in tags.orgTagDetails"
              :key="tag.tagId"
              class="w-[calc((100%-32px)/3)] cursor-pointer border border-#e5e7eb rounded-8px p-3 transition-colors dark:border-#333 hover:border-primary"
              @click="showModal(tag.tagId)"
            >
              <div class="flex items-center justify-between">
                <div class="font-medium">{{ tag.name }}</div>
                <NTag v-if="tag.tagId === tags.primaryOrg" type="primary" size="small" :bordered="false">
                  主标签
                  <template #icon>
                    <icon-solar:verified-check-bold-duotone class="text-icon" />
                  </template>
                </NTag>
              </div>
              <NEllipsis :line-clamp="3" class="mt-2 text-12px color-gray-400">{{ tag.description }}</NEllipsis>
            </div>
          </div>
        </NScrollbar>
      </NCard>

      <!-- 设置主标签弹窗 -->
      <NModal
        v-model:show="visible"
        :loading="submitLoading"
        preset="dialog"
        title="设置主标签"
        content="确定将当前标签设置为主标签吗？"
        positive-text="确认"
        negative-text="取消"
        @positive-click="setPrimaryOrg"
        @negative-click="visible = false"
      />

      <!-- 编辑资料弹窗 -->
      <NModal v-model:show="editVisible" preset="card" title="编辑资料" :style="{ width: '520px' }">
        <div class="flex flex-col gap-4 pt-2">
          <div class="flex flex-col gap-1">
            <span class="text-13px">昵称</span>
            <NInput v-model:value="editForm.nickname" placeholder="请输入昵称" />
          </div>
          <div class="flex flex-col gap-1">
            <span class="text-13px">邮箱</span>
            <NInput v-model:value="editForm.email" placeholder="请输入邮箱" />
          </div>
          <div class="flex flex-col gap-1">
            <span class="text-13px">手机号</span>
            <NInput v-model:value="editForm.phone" placeholder="请输入手机号" />
          </div>
          <div class="flex flex-col gap-1">
            <span class="text-13px">个人简介</span>
            <NInput v-model:value="editForm.bio" type="textarea" :rows="3" placeholder="一句话介绍自己" />
          </div>
        </div>
        <template #footer>
          <div class="flex justify-end gap-2">
            <NButton @click="editVisible = false">取消</NButton>
            <NButton type="primary" :loading="editLoading" @click="saveProfile">保存</NButton>
          </div>
        </template>
      </NModal>

      <!-- 修改密码弹窗 -->
      <NModal v-model:show="pwdVisible" preset="card" title="修改密码" :style="{ width: '440px' }">
        <div class="flex flex-col gap-4 pt-2">
          <NInput v-model:value="pwdForm.oldPassword" type="password" show-password-on="click" placeholder="原密码" />
          <NInput
            v-model:value="pwdForm.newPassword"
            type="password"
            show-password-on="click"
            placeholder="新密码（至少 6 位）"
          />
          <NInput
            v-model:value="pwdForm.confirmPassword"
            type="password"
            show-password-on="click"
            placeholder="确认新密码"
          />
        </div>
        <template #footer>
          <div class="flex justify-end gap-2">
            <NButton @click="pwdVisible = false">取消</NButton>
            <NButton type="primary" :loading="pwdLoading" @click="savePassword">确认</NButton>
          </div>
        </template>
      </NModal>
    </div>
  </NSpin>
</template>

<style scoped lang="scss">
:deep(.n-card__content) {
  flex: none !important;
  height: fit-content;
}
</style>
