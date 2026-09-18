<script setup lang="ts">
import type { FormRules } from 'naive-ui';

defineOptions({
  name: 'CreateUserDialog'
});

const emit = defineEmits<{ submitted: [] }>();

const visible = defineModel<boolean>('visible', { default: false });
const loading = ref(false);
const { formRef, validate, restoreValidation } = useNaiveForm();
const { defaultRequiredRule } = useFormRules();

type Model = {
  username: string;
  password: string;
  role: 'USER' | 'ADMIN';
};

const model = ref<Model>({
  username: '',
  password: '',
  role: 'USER'
});

const rules = ref<FormRules>({
  username: defaultRequiredRule,
  password: defaultRequiredRule
});

function close() {
  visible.value = false;
}

async function handleSubmit() {
  await validate();
  loading.value = true;
  const res = await request({
    method: 'POST',
    url: '/admin/users',
    data: model.value
  });
  if (!res.error) {
    window.$message?.success('用户创建成功');
    model.value.username = '';
    model.value.password = '';
    model.value.role = 'USER';
    close();
    emit('submitted');
  }
  loading.value = false;
}

watch(visible, () => {
  if (visible.value) {
    restoreValidation();
  }
});
</script>

<template>
  <NModal
    v-model:show="visible"
    preset="dialog"
    title="新增用户"
    :show-icon="false"
    :mask-closable="false"
    class="w-500px!"
    @positive-click="handleSubmit"
  >
    <NForm ref="formRef" :model="model" :rules="rules" label-placement="left" :label-width="100" mt-10>
      <NFormItem label="用户名" path="username">
        <NInput v-model:value="model.username" placeholder="请输入用户名" />
      </NFormItem>
      <NFormItem label="密码" path="password">
        <NInput v-model:value="model.password" type="password" show-password-on="click" placeholder="请输入密码" />
      </NFormItem>
      <NFormItem label="角色" path="role">
        <NSelect
          v-model:value="model.role"
          :options="[
            { label: '普通用户', value: 'USER' },
            { label: '管理员', value: 'ADMIN' }
          ]"
        />
      </NFormItem>
    </NForm>
    <template #action>
      <NSpace :size="16">
        <NButton @click="close">取消</NButton>
        <NButton type="primary" :loading="loading" @click="handleSubmit">创建</NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped></style>