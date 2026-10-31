<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { Lock, User } from '@element-plus/icons-vue'
import { getErrorMessage } from '../api/client'
import AppSettingsButton from '../components/AppSettingsButton.vue'
import { authState, bootstrapAuth, loginUser } from '../stores/auth'

const route = useRoute()
const router = useRouter()

const loginFormRef = ref<FormInstance>()
const loggingIn = ref(false)
const loginError = ref('')
const loginForm = reactive({
  username: '',
  password: '',
  remember: false,
})

const bootRetrying = ref(false)

const loginRules: FormRules = {
  username: [
    {
      required: true,
      whitespace: true,
      message: '请输入用户名',
      trigger: 'blur',
    },
  ],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
}

function safeRedirect(): string {
  const redirect = route.query.redirect
  if (
    typeof redirect === 'string' &&
    redirect.startsWith('/') &&
    !redirect.startsWith('//') &&
    !redirect.startsWith('/login')
  ) {
    return redirect
  }
  return '/'
}

async function handleLogin(): Promise<void> {
  if (loggingIn.value || !loginFormRef.value) return
  loggingIn.value = true
  loginError.value = ''
  try {
    let valid = false
    try {
      valid = await loginFormRef.value.validate()
    } catch {
      valid = false
    }
    if (!valid) return

    await loginUser({
      username: loginForm.username.trim(),
      password: loginForm.password,
      remember: loginForm.remember,
    })
    ElMessage.success('欢迎回来')
    await router.replace(safeRedirect())
  } catch (error) {
    loginError.value = getErrorMessage(error)
  } finally {
    loggingIn.value = false
  }
}

async function retryBootstrap(): Promise<void> {
  if (bootRetrying.value) return
  bootRetrying.value = true
  await bootstrapAuth()
  bootRetrying.value = false
  if (authState.user) await router.replace(safeRedirect())
}
</script>

<template>
  <div class="auth-page">
    <section class="auth-story">
      <div class="auth-story__ring" aria-hidden="true"></div>
      <div class="auth-story__inner">
        <div class="story-brand">
          <span class="story-brand__mark" aria-hidden="true">家</span>
          <span class="story-brand__copy">
            <strong>家庭账本</strong>
            <small>Household Ledger</small>
          </span>
        </div>
      </div>
    </section>

    <section class="auth-main">
      <div class="auth-card">
        <div class="auth-mobile-brand">
          <span class="auth-mobile-brand__mark" aria-hidden="true">家</span>
          <span>家庭账本</span>
        </div>

        <header class="auth-card__header">
          <h1>欢迎回来</h1>
          <AppSettingsButton />
        </header>

        <div v-if="authState.bootError" class="boot-alert" role="alert">
          <div class="boot-alert__copy">
            <strong>暂时无法连接服务器</strong>
            <p>{{ authState.bootError }}</p>
          </div>
          <el-button
            text
            type="primary"
            :loading="bootRetrying"
            @click="retryBootstrap"
          >
            重试
          </el-button>
        </div>

        <el-form
          ref="loginFormRef"
          :model="loginForm"
          :rules="loginRules"
          label-position="top"
          class="auth-form"
          @submit.prevent="handleLogin"
        >
          <el-form-item label="用户名" prop="username">
            <el-input
              v-model="loginForm.username"
              size="large"
              :prefix-icon="User"
              placeholder="请输入用户名"
              autocomplete="username"
              aria-label="用户名"
            />
          </el-form-item>

          <el-form-item label="密码" prop="password">
            <el-input
              v-model="loginForm.password"
              type="password"
              size="large"
              :prefix-icon="Lock"
              placeholder="请输入密码"
              autocomplete="current-password"
              show-password
              aria-label="密码"
            />
          </el-form-item>

          <div class="remember-row">
            <el-checkbox
              v-model="loginForm.remember"
              :disabled="loggingIn"
              class="remember-checkbox"
            >
              记住登录（30 天）
            </el-checkbox>
            <span class="remember-hint">仅在个人设备上勾选</span>
          </div>

          <el-alert
            v-if="loginError"
            class="form-alert"
            :title="loginError"
            type="error"
            :closable="false"
            show-icon
          />

          <el-button
            class="submit-button"
            type="primary"
            size="large"
            native-type="submit"
            :loading="loggingIn"
            :disabled="loggingIn"
            @click="handleLogin"
          >
            登录家庭账本
          </el-button>
        </el-form>
      </div>
    </section>
  </div>
</template>

<style scoped>
.auth-page {
  display: grid;
  grid-template-columns: minmax(0, 1.02fr) minmax(0, 0.98fr);
  min-height: 100vh;
  background: var(--hb-bg);
}

.auth-story {
  position: relative;
  display: flex;
  min-height: 100vh;
  overflow: hidden;
  color: #f7f3ea;
  background:
    radial-gradient(750px 520px at 8% 2%, rgba(255, 255, 255, 0.13), transparent 64%),
    linear-gradient(150deg, #0c5e57 0%, #0f6b62 47%, #113f3b 100%);
}

.auth-story::before {
  content: "";
  position: absolute;
  inset: 0;
  opacity: 0.14;
  background-image: linear-gradient(rgba(255, 255, 255, 0.24) 1px, transparent 1px);
  background-size: 100% 44px;
  mask-image: linear-gradient(to bottom, black, transparent 80%);
  pointer-events: none;
}

.auth-story__ring {
  position: absolute;
  right: -180px;
  bottom: -170px;
  width: 520px;
  height: 520px;
  border: 1px solid rgba(246, 240, 226, 0.22);
  border-radius: 50%;
  pointer-events: none;
}

.auth-story__ring::before,
.auth-story__ring::after {
  content: "";
  position: absolute;
  border: 1px solid rgba(246, 240, 226, 0.14);
  border-radius: 50%;
}

.auth-story__ring::before {
  inset: 62px;
}

.auth-story__ring::after {
  inset: 132px;
}

.auth-story__inner {
  position: relative;
  z-index: 1;
  display: flex;
  flex-direction: column;
  width: 100%;
  max-width: 610px;
  margin: 0 auto;
  padding: clamp(36px, 6vw, 64px) clamp(34px, 6vw, 72px);
}

.story-brand {
  display: flex;
  align-items: center;
  gap: 12px;
}

.story-brand__mark {
  display: grid;
  width: 42px;
  height: 42px;
  place-items: center;
  color: #0d5b54;
  background: #f5efe2;
  border-radius: 13px;
  font-family: var(--hb-display);
  font-size: 21px;
}

.story-brand__copy {
  display: flex;
  flex-direction: column;
  line-height: 1.25;
}

.story-brand__copy strong {
  font-family: var(--hb-display);
  font-size: 18px;
  font-weight: 600;
  letter-spacing: 0.04em;
}

.story-brand__copy small {
  margin-top: 3px;
  color: rgba(247, 243, 234, 0.56);
  font-size: 9px;
  letter-spacing: 0.13em;
  text-transform: uppercase;
}

.auth-main {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: clamp(24px, 5vw, 60px) clamp(20px, 4vw, 56px);
}

.auth-card {
  width: min(100%, 468px);
  padding: 34px 36px 28px;
  background: var(--hb-surface);
  border: 1px solid rgba(216, 205, 188, 0.8);
  border-radius: 24px;
  box-shadow:
    0 2px 4px rgba(59, 48, 33, 0.03),
    0 24px 70px rgba(59, 48, 33, 0.09);
}

.auth-mobile-brand {
  display: none;
  align-items: center;
  gap: 10px;
  margin-bottom: 24px;
  font-family: var(--hb-display);
  font-size: 17px;
  font-weight: 600;
  letter-spacing: 0.03em;
}

.auth-mobile-brand__mark {
  display: grid;
  width: 36px;
  height: 36px;
  place-items: center;
  color: #fffdf8;
  background: var(--hb-teal);
  border-radius: 11px;
  font-size: 18px;
}

.auth-card__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 22px;
}

.auth-card__header h1 {
  font-family: var(--hb-display);
  font-size: 28px;
  font-weight: 600;
  letter-spacing: 0.01em;
}

.boot-alert {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 18px;
  padding: 12px 14px;
  background: #fdf3e7;
  border: 1px solid #f0dcc0;
  border-radius: 12px;
}

.boot-alert__copy {
  flex: 1;
  min-width: 0;
}

.boot-alert__copy strong {
  color: #835c24;
  font-size: 12px;
  font-weight: 600;
}

.boot-alert__copy p {
  margin-top: 2px;
  color: #8f6f42;
  font-size: 12px;
  line-height: 1.6;
  overflow-wrap: anywhere;
}

.auth-form :deep(.el-form-item) {
  margin-bottom: 17px;
}

.auth-form :deep(.el-form-item__label) {
  padding-bottom: 6px;
  font-size: 12px;
  line-height: 1.4;
}

.auth-form :deep(.el-input__wrapper) {
  padding: 3px 13px;
  background: #fdfaf4;
  box-shadow: 0 0 0 1px var(--hb-line) inset;
}

.auth-form :deep(.el-input__wrapper:hover) {
  box-shadow: 0 0 0 1px var(--hb-line-strong) inset;
}

.auth-form :deep(.el-input__wrapper.is-focus) {
  box-shadow: 0 0 0 1px var(--hb-teal) inset;
}

.auth-form :deep(.el-input__inner) {
  height: 40px;
}

.remember-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 2px 10px;
  margin: -2px 0 16px;
}

.remember-row :deep(.el-checkbox) {
  height: auto;
  min-height: 24px;
  color: var(--hb-ink-soft);
  white-space: normal;
}

.remember-row :deep(.el-checkbox__label) {
  padding-left: 8px;
  font-size: 13px;
  line-height: 1.5;
}

.remember-hint {
  color: var(--hb-ink-faint);
  font-size: 12px;
  line-height: 1.5;
}

.form-alert {
  margin: 2px 0 16px;
}

.submit-button {
  width: 100%;
  height: 46px;
  margin-top: 3px;
  border-radius: 12px;
  font-size: 14px;
  letter-spacing: 0.03em;
  box-shadow: 0 10px 24px rgba(15, 118, 110, 0.16);
}

@media (max-width: 960px) {
  .auth-page {
    display: block;
    background:
      radial-gradient(700px 440px at 100% 0%, rgba(15, 118, 110, 0.1), transparent 60%),
      var(--hb-bg);
  }

  .auth-story {
    display: none;
  }

  .auth-main {
    min-height: 100vh;
    padding: 28px 20px;
  }

  .auth-mobile-brand {
    display: flex;
  }
}

@media (max-width: 520px) {
  .auth-main {
    padding: 16px 12px;
  }

  .auth-card {
    padding: 24px 20px 22px;
    border-radius: 20px;
  }

  .auth-card__header h1 {
    font-size: 24px;
  }
}
</style>
