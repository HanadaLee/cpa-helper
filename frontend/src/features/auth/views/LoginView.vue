<script setup lang="ts">
import { EyeIcon, EyeOffIcon, LogInIcon, TriangleAlertIcon } from '@lucide/vue'
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '@/components/ui/input-group'
import { Spinner } from '@/components/ui/spinner'
import { getSetupState, login, setupFirstAdmin } from '@/features/auth/api/authApi'
import AuthShell from '@/features/auth/components/AuthShell.vue'
import { setCurrentUser } from '@/features/auth/state/currentUser'
import { useI18n } from '@/shared/i18n'

const route = useRoute()
const router = useRouter()
const { errorText, t } = useI18n()
const isLoading = ref(false)
const isSetupLoading = ref(true)
const setupRequired = ref(false)
const casEnabled = ref(false)
const oidcEnabled = ref(false)
const showPassword = ref(false)
const errorMessage = ref<string | null>(null)
const form = reactive({
  username: '',
  password: '',
  nickname: '',
})
const headingTitle = computed(() => (setupRequired.value ? t('创建首个管理员账号', 'Create first admin account') : 'CPA-Helper'))
const headingSubtitle = computed(() =>
  setupRequired.value ? t('首次使用前需要先录入管理员账号', 'Create an admin account before first use') : t('本地 AI 用量管理控制台', 'Local AI usage management console'),
)
const submitText = computed(() => (setupRequired.value ? t('创建并登录', 'Create and sign in') : t('登录', 'Sign in')))
const casLoginHref = computed(() => {
  const redirect = typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/')
    ? route.query.redirect
    : '/'
  return `/cas/login?returnTo=${encodeURIComponent(redirect)}`
})
const oidcLoginHref = computed(() => casLoginHref.value.replace('/cas/login', '/oidc/login'))

onMounted(async () => {
  try {
    const state = await getSetupState()
    setupRequired.value = state.setup_required
    casEnabled.value = state.cas_enabled
    oidcEnabled.value = state.oidc_enabled
    if (route.query.sso_error === 'oidc') {
      errorMessage.value = t('OIDC 登录未完成，请重试。', 'OIDC sign-in was not completed. Please try again.')
    }
  } catch (error) {
    errorMessage.value = errorText(error, '初始化状态加载失败', 'Failed to load setup state')
  } finally {
    isSetupLoading.value = false
  }
})

async function handleSubmit() {
  if (setupRequired.value && !form.nickname.trim()) {
    toast.error(t('用户昵称不能为空', 'User nickname is required'))
    return
  }
  isLoading.value = true
  errorMessage.value = null
  try {
    let homePath = '/account/usage'
    if (setupRequired.value) {
      const user = await setupFirstAdmin({
        username: form.username,
        password: form.password,
        nickname: form.nickname,
      })
      setCurrentUser(user)
      homePath = user.is_admin ? '/admin/usage' : '/account/usage'
      toast.success(t('管理员账号已创建', 'Admin account created'))
    } else {
      const user = await login({ username: form.username, password: form.password })
      setCurrentUser(user)
      homePath = user.is_admin ? '/admin/usage' : '/account/usage'
      toast.success(t('登录成功', 'Signed in'))
    }
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : homePath
    await router.push(redirect)
  } catch (error) {
    errorMessage.value = errorText(error, '登录失败', 'Sign in failed')
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <AuthShell :section-label="t('登录区域', 'Sign-in area')">
    <Card class="w-full max-w-[420px]">
      <CardHeader class="text-center">
        <CardTitle>{{ headingTitle }}</CardTitle>
        <CardDescription>{{ headingSubtitle }}</CardDescription>
      </CardHeader>

      <CardContent class="flex flex-col gap-5">
        <Alert v-if="errorMessage" variant="destructive">
          <AlertDescription>{{ errorMessage }}</AlertDescription>
        </Alert>
        <Alert v-if="route.query.sso_logout === 'local'">
          <AlertDescription>{{ t('已退出本平台；身份服务未完成退出，如需切换账号请前往身份服务退出。', 'You have signed out locally. Sign out from the identity provider to switch accounts.') }}</AlertDescription>
        </Alert>

        <Alert v-if="setupRequired">
          <TriangleAlertIcon />
          <AlertTitle>{{ t('请注意', 'Important') }}</AlertTitle>
          <AlertDescription>
            {{ t('账号一旦创建，不允许删除，只允许禁用，请谨慎操作。', 'Accounts cannot be deleted after creation. They can only be disabled, so proceed carefully.') }}
          </AlertDescription>
        </Alert>

        <form @submit.prevent="handleSubmit">
          <FieldGroup>
            <Field>
              <FieldLabel for="login-username">{{ t('账号', 'Account') }}</FieldLabel>
              <Input id="login-username" v-model="form.username" autocomplete="username" />
            </Field>

            <Field>
              <FieldLabel for="login-password">{{ t('密码', 'Password') }}</FieldLabel>
              <InputGroup>
                <InputGroupInput
                  id="login-password"
                  v-model="form.password"
                  :type="showPassword ? 'text' : 'password'"
                  autocomplete="current-password"
                />
                <InputGroupAddon align="inline-end">
                  <InputGroupButton
                    size="icon-xs"
                    :aria-label="showPassword ? t('隐藏密码', 'Hide password') : t('显示密码', 'Show password')"
                    @click="showPassword = !showPassword"
                  >
                    <EyeOffIcon v-if="showPassword" data-icon="inline-start" />
                    <EyeIcon v-else data-icon="inline-start" />
                  </InputGroupButton>
                </InputGroupAddon>
              </InputGroup>
            </Field>

            <Field v-if="setupRequired">
              <FieldLabel for="login-nickname">{{ t('用户昵称', 'User nickname') }}</FieldLabel>
              <Input
                id="login-nickname"
                v-model="form.nickname"
                :placeholder="t('例如：研发用户', 'Example: Engineering user')"
              />
            </Field>

            <Button class="w-full" type="submit" :disabled="isLoading || isSetupLoading" :aria-busy="isLoading || isSetupLoading">
              <Spinner v-if="isLoading || isSetupLoading" data-icon="inline-start" />
              {{ submitText }}
            </Button>
          </FieldGroup>
        </form>

        <div v-if="(casEnabled || oidcEnabled) && !setupRequired" class="cas-login-section">
          <div class="cas-login-divider">
            <span>{{ t('或', 'OR') }}</span>
          </div>
          <Button v-if="oidcEnabled" as="a" class="w-full" variant="outline" :href="oidcLoginHref">
            <LogInIcon data-icon="inline-start" />
            {{ t('使用 OIDC 登录', 'Sign in with OIDC') }}
          </Button>
          <Button v-if="casEnabled" as="a" class="w-full" variant="outline" :href="casLoginHref">
            <LogInIcon data-icon="inline-start" />
            {{ t('使用 CAS 登录', 'Sign in with CAS') }}
          </Button>
        </div>
      </CardContent>
    </Card>
  </AuthShell>
</template>

<style scoped>
.cas-login-section {
  display: grid;
  gap: 1rem;
}

.cas-login-divider {
  display: flex;
  align-items: center;
  gap: .75rem;
  color: var(--muted-foreground);
  font-size: .75rem;
}

.cas-login-divider::before,
.cas-login-divider::after {
  height: 1px;
  flex: 1;
  background: var(--border);
  content: '';
}
</style>
