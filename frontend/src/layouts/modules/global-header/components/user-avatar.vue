<script setup lang="ts">
import { computed } from 'vue';
import type { VNode } from 'vue';
import { useAuthStore } from '@/store/modules/auth';
import { useRouterPush } from '@/hooks/common/router';
import { useSvgIcon } from '@/hooks/common/icon';
import { $t } from '@/locales';

defineOptions({
  name: 'UserAvatar'
});

const authStore = useAuthStore();
const { routerPushByKey, toLogin } = useRouterPush();
const { SvgIconVNode } = useSvgIcon();

const displayName = computed(() => authStore.userInfo.nickname || authStore.userInfo.username);

function loginOrRegister() {
  toLogin();
}

type DropdownKey = 'logout';

type DropdownOption =
  | {
      key: DropdownKey;
      label: string;
      icon?: () => VNode;
    }
  | {
      type: 'divider';
      key: string;
    };

const options = computed(() => {
  const opts: DropdownOption[] = [
    {
      label: $t('common.logout'),
      key: 'logout',
      icon: SvgIconVNode({ icon: 'material-symbols:logout-rounded', fontSize: 18 })
    }
  ];

  return opts;
});

const logoutVisible = ref(false);
const logoutLoading = ref(false);

async function handleLogout() {
  logoutLoading.value = true;
  await authStore.logout();
  logoutLoading.value = false;
  logoutVisible.value = false;
}

function handleDropdown(key: DropdownKey) {
  if (key === 'logout') {
    logoutVisible.value = true;
  } else {
    // If your other options are jumps from other routes, they will be directly supported here
    routerPushByKey(key);
  }
}
</script>

<template>
  <NButton v-if="!authStore.isLogin" quaternary @click="loginOrRegister">
    {{ $t('page.login.common.loginOrRegister') }}
  </NButton>
  <NDropdown v-else placement="bottom" trigger="click" :options="options" @select="handleDropdown">
    <div>
      <ButtonIcon>
        <SvgIcon icon="material-symbols:account-circle" class="text-icon-large" />
        <span class="text-16px font-medium">{{ displayName }}</span>
      </ButtonIcon>
    </div>
  </NDropdown>

  <NModal
    v-model:show="logoutVisible"
    preset="dialog"
    type="info"
    title="退出登录"
    content="确定要退出当前账号吗？"
    positive-text="退出登录"
    negative-text="取消"
    :loading="logoutLoading"
    :style="{ width: '360px' }"
    @positive-click="handleLogout"
    @negative-click="logoutVisible = false"
  />
</template>

<style scoped></style>
