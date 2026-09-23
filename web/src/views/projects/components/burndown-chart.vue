<script setup lang="ts">
defineOptions({ name: "BurndownChart" });

import { computed } from "vue";

import type { BurndownChart } from "@/api/types";

const props = defineProps<{
  chart: BurndownChart;
  height?: number;
}>();

const width = 640;
const height = computed(() => props.height ?? 220);
const padding = { top: 16, right: 16, bottom: 28, left: 36 };

const plotWidth = computed(() => width - padding.left - padding.right);
const plotHeight = computed(() => height.value - padding.top - padding.bottom);

const maxY = computed(() => Math.max(props.chart.total, 1));

const actualPoints = computed(() => {
  const points = props.chart.points ?? [];
  if (points.length === 0) return "";
  const step = points.length > 1 ? plotWidth.value / (points.length - 1) : 0;
  return points
    .map((point, index) => {
      const x = padding.left + index * step;
      const y = padding.top + plotHeight.value * (1 - point.remaining / maxY.value);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
});

const idealPoints = computed(() => {
  const points = props.chart.points ?? [];
  if (points.length < 2) return "";
  const endX = padding.left + plotWidth.value;
  const topY = padding.top + plotHeight.value * (1 - maxY.value / maxY.value);
  const bottomY = padding.top + plotHeight.value;
  return `${padding.left},${topY.toFixed(1)} ${endX},${bottomY.toFixed(1)}`;
});

const yAxisTicks = computed(() => {
  const ticks = [0, Math.ceil(maxY.value / 2), maxY.value];
  return [...new Set(ticks)].map((value) => ({
    value,
    y: padding.top + plotHeight.value * (1 - value / maxY.value),
  }));
});

const xLabels = computed(() => {
  const points = props.chart.points ?? [];
  if (points.length === 0) return [];
  const stride = Math.max(1, Math.ceil(points.length / 8));
  const labels: { label: string; x: number }[] = [];
  const step = points.length > 1 ? plotWidth.value / (points.length - 1) : 0;
  points.forEach((point, index) => {
    if (index % stride === 0 || index === points.length - 1) {
      labels.push({
        label: point.date.slice(5),
        x: padding.left + index * step,
      });
    }
  });
  return labels;
});
</script>

<template>
  <div class="burndown-chart">
    <svg :viewBox="`0 0 ${width} ${height}`" class="burndown-svg" role="img" aria-label="燃尽图">
      <!-- grid + y axis -->
      <line
        v-for="tick in yAxisTicks"
        :key="tick.value"
        :x1="padding.left"
        :x2="width - padding.right"
        :y1="tick.y"
        :y2="tick.y"
        class="burndown-grid"
      />
      <text
        v-for="tick in yAxisTicks"
        :key="'label-' + tick.value"
        :x="padding.left - 6"
        :y="tick.y + 4"
        class="burndown-axis-text"
        text-anchor="end"
      >
        {{ tick.value }}
      </text>
      <!-- x labels -->
      <text
        v-for="(item, index) in xLabels"
        :key="'x-' + index"
        :x="item.x"
        :y="height - 8"
        class="burndown-axis-text"
        text-anchor="middle"
      >
        {{ item.label }}
      </text>
      <!-- ideal line -->
      <polyline v-if="idealPoints" :points="idealPoints" class="burndown-ideal" />
      <!-- actual line -->
      <polyline v-if="actualPoints" :points="actualPoints" class="burndown-actual" />
    </svg>
    <div class="burndown-legend">
      <span class="burndown-legend-item">
        <i class="burndown-swatch burndown-swatch--actual" />实际剩余 {{ chart.total }} 项
      </span>
      <span class="burndown-legend-item">
        <i class="burndown-swatch burndown-swatch--ideal" />理想燃尽
      </span>
    </div>
  </div>
</template>

<style scoped lang="scss">
.burndown-chart {
  width: 100%;
}

.burndown-svg {
  width: 100%;
  height: auto;
}

.burndown-grid {
  stroke: var(--u-border-color-light, #e5e7eb);
  stroke-width: 1;
}

.burndown-axis-text {
  fill: var(--u-text-color-assist, #7c8494);
  font-size: 11px;
}

.burndown-ideal {
  fill: none;
  stroke: var(--u-text-color-assist, #9ca3af);
  stroke-width: 1.5;
  stroke-dasharray: 6 4;
}

.burndown-actual {
  fill: none;
  stroke: var(--u-color-primary, #3b82f6);
  stroke-width: 2.5;
  stroke-linejoin: round;
}

.burndown-legend {
  display: flex;
  gap: 16px;
  margin-top: 8px;
  font-size: 12px;
  color: var(--u-text-color-assist, #7c8494);
}

.burndown-legend-item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.burndown-swatch {
  display: inline-block;
  width: 16px;
  height: 3px;
  border-radius: 2px;

  &--actual {
    background: var(--u-color-primary, #3b82f6);
  }

  &--ideal {
    background: var(--u-text-color-assist, #9ca3af);
  }
}
</style>
