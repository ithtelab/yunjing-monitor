<script setup>
/**
 * MarketGuide —— 市场使用指南弹窗
 *
 * 内容严格按本站真实业务编写（买家浏览 / 卖家四步上架 / 我的上架管理 / 常见问题）。
 * 排版要点（针对长文扫读）：
 *   - 区块标题：蓝色圆点标记（对齐站点 panel-title 风格）
 *   - 上架流程：蓝色数字徽章步骤卡，一眼看清先后
 *   - 关键信息：.hl 主题蓝高亮，不再只是"更黑的灰"
 *   - FAQ：问句深色加粗、答案次要色，逐条分隔
 *
 * 事件：
 *   close     —— 关闭弹窗
 *   go-submit —— 点击「立即上架」：先关闭，再由父级决定是否跳上架页
 */
import AppDialog from '@/components/ui/AppDialog.vue'
import BaseButton from '@/components/ui/BaseButton.vue'
import ParticleButton from '@/components/ui/ParticleButton.vue'
import { useI18n } from 'vue-i18n'

defineOptions({ name: 'MarketGuide' })

const { t } = useI18n()

const props = defineProps({
  open: { type: Boolean, default: false }
})
const emit = defineEmits(['close', 'go-submit'])

const goSubmit = () => {
  emit('close')
  emit('go-submit')
}
</script>

<template>
  <AppDialog :open="open" :title="t('guide-title')" @close="emit('close')">
    <div class="guide">
      <section class="guide__item">
        <strong>{{ t('guide-s1-title') }}</strong>
        <p v-html="t('guide-s1-desc')"></p>
      </section>

      <section class="guide__item">
        <strong>{{ t('guide-s2-title') }}</strong>
        <p v-html="t('guide-s2-desc')"></p>
      </section>

      <section class="guide__item">
        <strong>{{ t('guide-s3-title') }}</strong>
        <div class="guide__steps">
          <div class="guide__step">
            <span class="guide__num">1</span>
            <p v-html="t('guide-s3-step1')"></p>
          </div>
          <div class="guide__step">
            <span class="guide__num">2</span>
            <p v-html="t('guide-s3-step2')"></p>
          </div>
          <div class="guide__step">
            <span class="guide__num">3</span>
            <p v-html="t('guide-s3-step3')"></p>
          </div>
          <div class="guide__step">
            <span class="guide__num">4</span>
            <p v-html="t('guide-s3-step4')"></p>
          </div>
        </div>
      </section>

      <section class="guide__item">
        <strong>{{ t('guide-s4-title') }}</strong>
        <p v-html="t('guide-s4-desc')"></p>
      </section>

      <section class="guide__item">
        <strong>{{ t('guide-faq-title') }}</strong>
        <div class="guide__faq">
          <p>
            <b>{{ t('guide-faq1-q') }}</b>
            <span>{{ t('guide-faq1-a') }}</span>
          </p>
          <p>
            <b>{{ t('guide-faq2-q') }}</b>
            <span>{{ t('guide-faq2-a') }}</span>
          </p>
          <p>
            <b>{{ t('guide-faq3-q') }}</b>
            <span v-html="t('guide-faq3-a')"></span>
          </p>
          <p>
            <b>{{ t('guide-faq4-q') }}</b>
            <span>{{ t('guide-faq4-a') }}</span>
          </p>
        </div>
      </section>
    </div>

    <template #footer>
      <BaseButton @click="emit('close')">{{ t('common-close') }}</BaseButton>
      <ParticleButton variant="primary" :show-icon="false" @click="goSubmit">{{ t('guide-go-submit') }}</ParticleButton>
    </template>
  </AppDialog>
</template>

<style scoped>
.guide {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

/* 区块标题：蓝色圆点标记（对齐全站 panel-title 风格） */
.guide__item > strong {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  font-size: 14px;
  font-weight: 700;
  color: var(--color-text-1, #1d2129);
}

.guide__item > strong::before {
  content: '';
  flex: none;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #165dff;
}

/* 正文比默认次要色深一档，长文不糊 */
.guide__item p {
  margin: 0;
  font-size: 13px;
  line-height: 1.75;
  color: var(--color-text-2, #4e5969);
}

/* 关键信息高亮：主题蓝 + 加粗 */
.hl {
  color: #165dff;
  font-weight: 600;
}

/* 上架四步：数字徽章步骤卡 */
.guide__steps {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.guide__step {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 9px 12px;
  border-radius: 10px;
  background: var(--color-fill-1, #f7f8fa);
}

.guide__num {
  flex: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  margin-top: 1px;
  border-radius: 50%;
  background: #165dff;
  color: #fff;
  font-size: 12px;
  font-weight: 700;
}

.guide__step p {
  flex: 1;
  min-width: 0;
}

/* FAQ：问句深色加粗、答案次要色，逐条分隔 */
.guide__faq {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.guide__faq p b {
  display: block;
  margin-bottom: 2px;
  font-size: 13px;
  font-weight: 600;
  color: var(--color-text-1, #1d2129);
}

.guide__faq p span {
  display: block;
}

/* 深色模式 */
body[arco-theme='dark'] .hl {
  color: #7aa5ff;
}

body[arco-theme='dark'] .guide__step {
  background: rgba(255, 255, 255, 0.05);
}
</style>
