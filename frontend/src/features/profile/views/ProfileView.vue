<script setup lang="ts">
import { EyeIcon, EyeOffIcon, KeyRoundIcon, ShieldCheckIcon } from '@lucide/vue'
import { computed, onMounted, reactive, ref } from 'vue'
import { toast } from 'vue-sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '@/components/ui/input-group'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { changeCredentials, getMe } from '@/features/auth/api/authApi'
import { useCurrentUser } from '@/features/auth/state/currentUser'
import { useI18n } from '@/shared/i18n'
import { formatDateTime } from '@/shared/utils/format'

const { currentUser, setCurrentUser } = useCurrentUser()
const { errorText, t } = useI18n()
const isLoading = ref(currentUser.value === null)
const isPasswordDialogOpen = ref(false)
const isSavingPassword = ref(false)
const passwordError = ref<string | null>(null)
const showCurrentPassword = ref(false)
const showNewPassword = ref(false)
const showConfirmPassword = ref(false)
const passwordForm = reactive({
  currentPassword: '',
  newPassword: '',
  confirmPassword: '',
})

const displayName = computed(() => currentUser.value?.nickname?.trim() || currentUser.value?.username || '-')
const secondaryIdentity = computed(() => currentUser.value?.email?.trim() || currentUser.value?.username || '-')
const avatarInitials = computed(() => {
  const characters = Array.from(displayName.value.trim())
  return characters.slice(0, 2).join('').toUpperCase() || 'U'
})

onMounted(async () => {
  try {
    setCurrentUser(await getMe())
  } catch {
    // The global route guard handles expired sessions.
  } finally {
    isLoading.value = false
  }
})

function resetPasswordForm() {
  passwordForm.currentPassword = ''
  passwordForm.newPassword = ''
  passwordForm.confirmPassword = ''
  passwordError.value = null
  showCurrentPassword.value = false
  showNewPassword.value = false
  showConfirmPassword.value = false
}

function openPasswordDialog() {
  resetPasswordForm()
  isPasswordDialogOpen.value = true
}

function handlePasswordDialogChange(open: boolean) {
  isPasswordDialogOpen.value = open
  if (!open) {
    resetPasswordForm()
  }
}

async function savePassword() {
  passwordError.value = null
  if (passwordForm.newPassword.length < 8) {
    passwordError.value = t('密码长度不能少于 8 位', 'Password must be at least 8 characters')
    return
  }
  if (passwordForm.newPassword !== passwordForm.confirmPassword) {
    passwordError.value = t('两次输入的新密码不一致', 'The new passwords do not match')
    return
  }
  isSavingPassword.value = true
  try {
    const user = await changeCredentials({
      current_password: passwordForm.currentPassword,
      password: passwordForm.newPassword,
    })
    setCurrentUser(user)
    isPasswordDialogOpen.value = false
    resetPasswordForm()
    toast.success(t('密码已更新', 'Password updated'))
  } catch (error) {
    passwordError.value = errorText(error, '更新密码失败', 'Failed to update password')
  } finally {
    isSavingPassword.value = false
  }
}
</script>

<template>
  <section class="profile-page">
    <div class="page-toolbar">
      <h1 data-page-title class="page-title">{{ t('个人资料', 'Profile') }}</h1>
    </div>

    <div class="profile-grid">
      <Card>
        <CardHeader>
          <CardTitle>{{ t('账户信息', 'Account information') }}</CardTitle>
          <CardDescription>{{ t('当前登录账户的基本资料。', 'Basic information for the signed-in account.') }}</CardDescription>
        </CardHeader>
        <CardContent>
          <div v-if="isLoading" class="profile-summary">
            <Skeleton class="size-14 rounded-full" />
            <div class="grid min-w-0 flex-1 gap-2">
              <Skeleton class="h-4 w-36" />
              <Skeleton class="h-3 w-52 max-w-full" />
              <Skeleton class="h-3 w-40 max-w-full" />
            </div>
          </div>
          <div v-else-if="currentUser" class="profile-summary">
            <Avatar class="size-14">
              <AvatarImage v-if="currentUser.avatar" :src="currentUser.avatar" alt="" />
              <AvatarFallback class="text-base">{{ avatarInitials }}</AvatarFallback>
            </Avatar>
            <div class="profile-identity">
              <strong>{{ displayName }}</strong>
              <span>{{ secondaryIdentity }}</span>
              <span>{{ t('注册时间：', 'Registered: ') }}{{ formatDateTime(currentUser.created_at) }}</span>
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{{ t('登录安全', 'Sign-in security') }}</CardTitle>
          <CardDescription>{{ t('查看登录方式并维护可用的本地凭据。', 'Review the sign-in method and maintain available local credentials.') }}</CardDescription>
          <CardAction v-if="currentUser" class="flex flex-wrap gap-2">
            <Badge v-if="currentUser.oidc_bound">{{ t('OIDC 已绑定', 'OIDC connected') }}</Badge>
            <Badge v-if="currentUser.cas_bound">{{ t('CAS 已绑定', 'CAS connected') }}</Badge>
            <Badge v-if="!currentUser.cas_bound && !currentUser.oidc_bound" variant="secondary">{{ t('本地账号', 'Local account') }}</Badge>
          </CardAction>
        </CardHeader>
        <CardContent v-if="isLoading" class="grid gap-3">
          <Skeleton class="h-4 w-3/4" />
          <Skeleton class="h-9 w-28" />
        </CardContent>
        <CardContent v-else-if="currentUser" class="security-content">
          <div class="security-message">
            <ShieldCheckIcon />
            <p v-if="currentUser.cas_bound || currentUser.oidc_bound">
              {{ t('当前账号已绑定单点登录，绑定关系不支持解绑。', 'This account is connected to single sign-on and cannot be disconnected.') }}
            </p>
            <p v-else>
              {{ t('当前账号使用本地登录凭据。', 'This account uses local sign-in credentials.') }}
            </p>
          </div>

          <Button v-if="currentUser.can_change_password" variant="outline" @click="openPasswordDialog">
            <KeyRoundIcon data-icon="inline-start" />
            {{ t('修改密码', 'Change password') }}
          </Button>
          <p v-else class="credential-note">
            {{ t('本地密码修改已关闭；如需修改登录凭据，请前往统一账号中心。', 'Local password changes are disabled. Manage sign-in credentials in the identity provider account center.') }}
          </p>
        </CardContent>
      </Card>
    </div>

    <Dialog :open="isPasswordDialogOpen" @update:open="handlePasswordDialogChange">
      <DialogContent class="sm:max-w-[440px]">
        <DialogHeader>
          <DialogTitle>{{ t('修改密码', 'Change password') }}</DialogTitle>
          <DialogDescription>{{ t('验证当前密码后设置新的本地登录密码。', 'Verify the current password, then set a new local sign-in password.') }}</DialogDescription>
        </DialogHeader>

        <Alert v-if="passwordError" variant="destructive">
          <AlertDescription>{{ passwordError }}</AlertDescription>
        </Alert>

        <FieldGroup>
          <Field>
            <FieldLabel for="profile-current-password">{{ t('当前密码', 'Current password') }}</FieldLabel>
            <InputGroup>
              <InputGroupInput
                id="profile-current-password"
                v-model="passwordForm.currentPassword"
                :type="showCurrentPassword ? 'text' : 'password'"
                autocomplete="current-password"
              />
              <InputGroupAddon align="inline-end">
                <InputGroupButton
                  size="icon-xs"
                  :aria-label="showCurrentPassword ? t('隐藏密码', 'Hide password') : t('显示密码', 'Show password')"
                  @click="showCurrentPassword = !showCurrentPassword"
                >
                  <EyeOffIcon v-if="showCurrentPassword" />
                  <EyeIcon v-else />
                </InputGroupButton>
              </InputGroupAddon>
            </InputGroup>
          </Field>

          <Field>
            <FieldLabel for="profile-new-password">{{ t('新密码', 'New password') }}</FieldLabel>
            <InputGroup>
              <InputGroupInput
                id="profile-new-password"
                v-model="passwordForm.newPassword"
                :type="showNewPassword ? 'text' : 'password'"
                autocomplete="new-password"
              />
              <InputGroupAddon align="inline-end">
                <InputGroupButton
                  size="icon-xs"
                  :aria-label="showNewPassword ? t('隐藏密码', 'Hide password') : t('显示密码', 'Show password')"
                  @click="showNewPassword = !showNewPassword"
                >
                  <EyeOffIcon v-if="showNewPassword" />
                  <EyeIcon v-else />
                </InputGroupButton>
              </InputGroupAddon>
            </InputGroup>
          </Field>

          <Field>
            <FieldLabel for="profile-confirm-password">{{ t('确认新密码', 'Confirm new password') }}</FieldLabel>
            <InputGroup>
              <InputGroupInput
                id="profile-confirm-password"
                v-model="passwordForm.confirmPassword"
                :type="showConfirmPassword ? 'text' : 'password'"
                autocomplete="new-password"
                @keydown.enter="savePassword"
              />
              <InputGroupAddon align="inline-end">
                <InputGroupButton
                  size="icon-xs"
                  :aria-label="showConfirmPassword ? t('隐藏密码', 'Hide password') : t('显示密码', 'Show password')"
                  @click="showConfirmPassword = !showConfirmPassword"
                >
                  <EyeOffIcon v-if="showConfirmPassword" />
                  <EyeIcon v-else />
                </InputGroupButton>
              </InputGroupAddon>
            </InputGroup>
          </Field>
        </FieldGroup>

        <DialogFooter>
          <Button variant="outline" :disabled="isSavingPassword" @click="handlePasswordDialogChange(false)">
            {{ t('取消', 'Cancel') }}
          </Button>
          <Button :disabled="isSavingPassword" @click="savePassword">
            <Spinner v-if="isSavingPassword" data-icon="inline-start" />
            {{ t('保存', 'Save') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </section>
</template>

<style scoped>
.profile-page {
  display: grid;
  gap: 20px;
}

.profile-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.profile-summary {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 16px;
}

.profile-identity {
  display: grid;
  min-width: 0;
  gap: 3px;
}

.profile-identity strong,
.profile-identity span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.profile-identity strong {
  color: var(--cpa-text-strong);
  font-weight: 600;
}

.profile-identity span {
  color: var(--cpa-text-muted);
  font-size: 13px;
}

.security-content {
  display: grid;
  justify-items: start;
  gap: 16px;
}

.security-message {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  color: var(--cpa-text-muted);
  font-size: 14px;
}

.security-message svg {
  width: 18px;
  height: 18px;
  flex: 0 0 auto;
  color: var(--primary);
}

.security-message p,
.credential-note {
  margin: 0;
}

.credential-note {
  color: var(--cpa-text-muted);
  font-size: 14px;
}

@media (max-width: 900px) {
  .profile-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
