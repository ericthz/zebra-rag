<script setup lang="tsx">
import { NButton, NPopconfirm, NTag } from 'naive-ui';
import UserSearch from './modules/user-search.vue';
import OrgTagSettingDialog from './modules/org-tag-setting-dialog.vue';
import CreateUserDialog from './modules/create-user-dialog.vue';

const appStore = useAppStore();

function apiFn(params: Api.User.SearchParams) {
  return request<Api.User.List>({ url: '/admin/users/list', params });
}

const { columns, columnChecks, data, getData, loading, mobilePagination, searchParams, resetSearchParams } = useTable({
  apiFn,
  apiParams: {
    keyword: null,
    orgTag: null,
    status: null
  },
  columns: () => [
    {
      key: 'index',
      title: '序号',
      width: 64
    },
    {
      key: 'username',
      title: '用户名',
      minWidth: 100,
      ellipsis: {
        tooltip: true
      }
    },
    {
      key: 'nickname',
      title: '昵称',
      minWidth: 100,
      ellipsis: {
        tooltip: true
      },
      render: row => row.nickname || '-'
    },
    {
      key: 'orgTags',
      title: '标签',
      ellipsis: {
        tooltip: true
      },
      render: row => (
        <div class="flex flex-nowrap gap-2 overflow-hidden">
          {row.orgTags.map(tag => (
            <NTag key={tag.tagId} bordered={false} type={tag.tagId === row.primaryOrg ? 'primary' : 'default'}>
              {tag.name}
            </NTag>
          ))}
        </div>
      )
    },
    {
      key: 'phone',
      title: '手机号',
      width: 140,
      ellipsis: {
        tooltip: true
      },
      render: row => row.phone || '-'
    },
    {
      key: 'email',
      title: '邮箱',
      width: 200,
      ellipsis: {
        tooltip: true
      },
      render: row => row.email || '-'
    },
    {
      key: 'status',
      title: '是否启用',
      width: 100,
      render: row => (
        <NTag bordered={false} type={row.status ? 'success' : 'warning'}>
          {row.status ? '已启用' : '已禁用'}
        </NTag>
      )
    },
    {
      key: 'createdAt',
      title: '创建时间',
      width: 200,
      render: row => dayjs(row.createdAt).format('YYYY-MM-DD HH:mm:ss')
    },
    {
      key: 'operate',
      title: '操作',
      width: 220,
      render: row => (
        <div class="flex items-center gap-8px">
          <NButton type="primary" ghost size="small" onClick={() => handleOrgTag(row)}>
            分配组织标签
          </NButton>
          <NPopconfirm positive-text="删除" negative-text="取消" onPositiveClick={() => handleDelete(row)}>
            {{
              default: () => <span>确认删除该用户？</span>,
              trigger: () => (
                <NButton type="error" ghost size="small" disabled={row.username === 'admin'}>
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

const visible = ref(false);
const editingData = ref<Api.User.Item | null>(null);
function handleOrgTag(row: Api.User.Item) {
  editingData.value = row;
  visible.value = true;
}

const createVisible = ref(false);
function handleCreate() {
  createVisible.value = true;
}

async function handleDelete(row: Api.User.Item) {
  const { error } = await request({ method: 'DELETE', url: `/admin/users/${row.userId}` });
  if (!error) {
    window.$message?.success('用户删除成功');
    await getData();
  }
}

// async function setPrimaryOrgTag(userId: string, primaryOrg: string) {
//   loading.value = true;
//   const { error } = await request({ url: 'users/primary-org', method: 'PUT', data: { primaryOrg, userId } });
//   if (!error) {
//     window.$message?.success('操作成功');
//     await getData();
//   }
//   loading.value = false;
// }
</script>

<template>
  <div class="min-h-500px flex-col-stretch gap-16px overflow-hidden lt-sm:overflow-auto">
    <Teleport defer to="#header-extra">
      <UserSearch v-model:model="searchParams" @reset="resetSearchParams" @search="getData" />
    </Teleport>
    <NCard title="用户列表" :bordered="false" size="small" class="sm:flex-1-hidden card-wrapper">
      <template #header-extra>
        <TableHeaderOperation v-model:columns="columnChecks" addable :loading="loading" @add="handleCreate" @refresh="getData" />
      </template>
      <NDataTable
        :columns="columns"
        :data="data"
        size="small"
        :flex-height="!appStore.isMobile"
        :scroll-x="1210"
        :loading="loading"
        remote
        :row-key="row => row.id"
        :pagination="mobilePagination"
        class="sm:h-full"
      />
    </NCard>
    <OrgTagSettingDialog v-model:visible="visible" :row-data="editingData!" @submitted="getData" />
    <CreateUserDialog v-model:visible="createVisible" @submitted="getData" />
  </div>
</template>

<style scoped></style>
