import { defineComponent, computed, mergeProps, unref, useSSRContext, ref, watch } from "vue";
import { ssrRenderAttrs, ssrInterpolate, ssrRenderComponent, ssrRenderClass, ssrRenderList } from "vue/server-renderer";
import { Line } from "vue-chartjs";
import { Chart, CategoryScale, LinearScale, PointElement, LineElement, Title, Tooltip } from "chart.js";
const _sfc_main$3 = /* @__PURE__ */ defineComponent({
  __name: "StatsCards",
  __ssrInlineRender: true,
  props: {
    totalSent: {},
    totalConfirmed: {},
    totalErrors: {},
    tps: {},
    uptimeSecs: {},
    activeWorkers: {}
  },
  setup(__props) {
    const props = __props;
    const uptime = computed(() => {
      const m = Math.floor(props.uptimeSecs / 60);
      const s = props.uptimeSecs % 60;
      return `${m}m ${s.toString().padStart(2, "0")}s`;
    });
    const errorRate = computed(() => {
      const total = props.totalConfirmed + props.totalErrors;
      if (total === 0) return "—";
      return (props.totalErrors / total * 100).toFixed(2) + "%";
    });
    const progress = computed(() => {
      if (props.totalSent === 0) return 0;
      return Math.min(100, props.totalConfirmed / props.totalSent * 100);
    });
    const progressBar = computed(() => {
      const p = progress.value;
      const filled = Math.round(p / 10);
      const empty = 10 - filled;
      return "[" + "█".repeat(filled) + "░".repeat(empty) + "]";
    });
    return (_ctx, _push, _parent, _attrs) => {
      _push(`<div${ssrRenderAttrs(mergeProps({ class: "grid grid-cols-7 gap-px bg-[#222] mb-4" }, _attrs))}><div class="bg-black p-2"><div class="text-[11px] text-gray-600 uppercase tracking-[0.05em]">sent</div><div class="text-sm text-gray-200 font-mono tabular-nums mt-0.5">${ssrInterpolate(__props.totalSent.toLocaleString())}</div></div><div class="bg-black p-2"><div class="text-[11px] text-gray-600 uppercase tracking-[0.05em]">confirmed</div><div class="text-sm text-green-400 font-mono tabular-nums mt-0.5">${ssrInterpolate(__props.totalConfirmed.toLocaleString())}</div></div><div class="bg-black p-2"><div class="text-[11px] text-gray-600 uppercase tracking-[0.05em]">errors</div><div class="text-sm text-red-400 font-mono tabular-nums mt-0.5">${ssrInterpolate(__props.totalErrors.toLocaleString())} <span class="text-gray-600 text-[11px] ml-1">${ssrInterpolate(unref(errorRate))}</span></div></div><div class="bg-black p-2"><div class="text-[11px] text-gray-600 uppercase tracking-[0.05em]">tps</div><div class="text-sm text-yellow-400 font-mono tabular-nums mt-0.5">${ssrInterpolate(__props.tps.toFixed(0))}</div></div><div class="bg-black p-2"><div class="text-[11px] text-gray-600 uppercase tracking-[0.05em]">workers</div><div class="text-sm text-gray-200 font-mono tabular-nums mt-0.5">${ssrInterpolate(__props.activeWorkers)}</div></div><div class="bg-black p-2"><div class="text-[11px] text-gray-600 uppercase tracking-[0.05em]">uptime</div><div class="text-sm text-gray-200 font-mono tabular-nums mt-0.5">${ssrInterpolate(unref(uptime))}</div></div><div class="bg-black p-2"><div class="text-[11px] text-gray-600 uppercase tracking-[0.05em]">progress</div><div class="font-mono text-gray-200 mt-0.5"><span class="text-xs">${ssrInterpolate(unref(progressBar))}</span><span class="text-[11px] text-gray-500 ml-1">${ssrInterpolate(unref(progress).toFixed(0))}%</span></div></div></div>`);
    };
  }
});
const _sfc_setup$3 = _sfc_main$3.setup;
_sfc_main$3.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add("components/StatsCards.vue");
  return _sfc_setup$3 ? _sfc_setup$3(props, ctx) : void 0;
};
const _sfc_main$2 = /* @__PURE__ */ defineComponent({
  __name: "TpsChart",
  __ssrInlineRender: true,
  props: {
    data: {}
  },
  setup(__props) {
    Chart.register(CategoryScale, LinearScale, PointElement, LineElement, Title, Tooltip);
    const props = __props;
    const chartData = computed(() => ({
      labels: props.data.map((d) => d.time),
      datasets: [
        {
          label: "TPS",
          data: props.data.map((d) => d.value),
          borderColor: "#eab308",
          pointRadius: 0,
          borderWidth: 1
        }
      ]
    }));
    const chartOptions = {
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      scales: {
        x: {
          display: true,
          grid: { color: "rgba(255,255,255,0.03)" },
          ticks: { color: "#666", maxTicksLimit: 8, font: { size: 10 } }
        },
        y: {
          beginAtZero: true,
          grid: { color: "rgba(255,255,255,0.03)" },
          ticks: { color: "#666", font: { size: 10 } }
        }
      },
      plugins: {
        legend: { display: false },
        tooltip: { enabled: false }
      }
    };
    return (_ctx, _push, _parent, _attrs) => {
      _push(`<div${ssrRenderAttrs(mergeProps({ class: "bg-black border border-[#222] p-2" }, _attrs))}><div class="text-[11px] text-gray-600 uppercase tracking-[0.05em] mb-1">tps (60s)</div><div class="h-36">`);
      if (__props.data.length) {
        _push(ssrRenderComponent(unref(Line), {
          data: unref(chartData),
          options: chartOptions
        }, null, _parent));
      } else {
        _push(`<div class="flex items-center justify-center h-full text-gray-700 text-xs">—</div>`);
      }
      _push(`</div></div>`);
    };
  }
});
const _sfc_setup$2 = _sfc_main$2.setup;
_sfc_main$2.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add("components/TpsChart.vue");
  return _sfc_setup$2 ? _sfc_setup$2(props, ctx) : void 0;
};
const _sfc_main$1 = /* @__PURE__ */ defineComponent({
  __name: "CumulativeChart",
  __ssrInlineRender: true,
  props: {
    data: {}
  },
  setup(__props) {
    const props = __props;
    const chartData = computed(() => ({
      labels: props.data.map((d) => d.time),
      datasets: [
        {
          label: "Confirmed",
          data: props.data.map((d) => d.value),
          borderColor: "#22c55e",
          pointRadius: 0,
          borderWidth: 1
        }
      ]
    }));
    const chartOptions = {
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      scales: {
        x: {
          display: true,
          grid: { color: "rgba(255,255,255,0.03)" },
          ticks: { color: "#666", maxTicksLimit: 8, font: { size: 10 } }
        },
        y: {
          beginAtZero: true,
          grid: { color: "rgba(255,255,255,0.03)" },
          ticks: { color: "#666", font: { size: 10 } }
        }
      },
      plugins: {
        legend: { display: false },
        tooltip: { enabled: false }
      }
    };
    return (_ctx, _push, _parent, _attrs) => {
      _push(`<div${ssrRenderAttrs(mergeProps({ class: "bg-black border border-[#222] p-2" }, _attrs))}><div class="text-[11px] text-gray-600 uppercase tracking-[0.05em] mb-1">cumulative (120s)</div><div class="h-36">`);
      if (__props.data.length) {
        _push(ssrRenderComponent(unref(Line), {
          data: unref(chartData),
          options: chartOptions
        }, null, _parent));
      } else {
        _push(`<div class="flex items-center justify-center h-full text-gray-700 text-xs">—</div>`);
      }
      _push(`</div></div>`);
    };
  }
});
const _sfc_setup$1 = _sfc_main$1.setup;
_sfc_main$1.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add("components/CumulativeChart.vue");
  return _sfc_setup$1 ? _sfc_setup$1(props, ctx) : void 0;
};
let source = null;
const useStats = () => {
  const stats = ref({
    total_sent: 0,
    total_confirmed: 0,
    total_errors: 0,
    tps: 0,
    active_workers: 0,
    started_at: "",
    uptime_secs: 0
  });
  const tpsHistory = ref([]);
  const cumulativeHistory = ref([]);
  const eventLog = ref([]);
  const sseConnected = ref(false);
  const lastUpdated = ref("");
  const statsUrl = "http://localhost:9090";
  const log = (msg) => {
    const t = (/* @__PURE__ */ new Date()).toLocaleTimeString();
    eventLog.value.unshift(`[${t}] ${msg}`);
    if (eventLog.value.length > 100) eventLog.value.pop();
  };
  const connectSSE = () => {
    if (source) source.close();
    source = new EventSource(`${statsUrl}/api/events`);
    source.onopen = () => {
      sseConnected.value = true;
      log("sse connected");
    };
    source.addEventListener("stats", (event) => {
      const data = JSON.parse(event.data);
      stats.value = data;
      lastUpdated.value = (/* @__PURE__ */ new Date()).toLocaleTimeString();
      const now = (/* @__PURE__ */ new Date()).toLocaleTimeString();
      tpsHistory.value.push({ time: now, value: data.tps });
      if (tpsHistory.value.length > 60) tpsHistory.value.shift();
      cumulativeHistory.value.push({ time: now, value: data.total_confirmed });
      if (cumulativeHistory.value.length > 120) cumulativeHistory.value.shift();
    });
    source.onerror = () => {
      sseConnected.value = false;
    };
  };
  const startGeneration = async (count = 1e4, speed = 100) => {
    log(`start count=${count} speed=${speed}`);
    try {
      await fetch(`${statsUrl}/start?count=${count}&speed=${speed}`, {
        method: "POST"
      });
    } catch {
      log("start failed");
    }
  };
  const stopGeneration = async () => {
    log("stop");
    try {
      await fetch(`${statsUrl}/stop`, { method: "POST" });
    } catch {
      log("stop failed");
    }
  };
  return {
    stats,
    tpsHistory,
    cumulativeHistory,
    eventLog,
    sseConnected,
    lastUpdated,
    connectSSE,
    startGeneration,
    stopGeneration
  };
};
const _sfc_main = /* @__PURE__ */ defineComponent({
  __name: "index",
  __ssrInlineRender: true,
  setup(__props) {
    const {
      stats,
      tpsHistory,
      cumulativeHistory,
      eventLog,
      sseConnected,
      lastUpdated
    } = useStats();
    const generating = ref(false);
    watch(() => stats.value.total_confirmed, (val, old) => {
      if (val > 0 && val === stats.value.total_sent && old > 0) {
        generating.value = false;
      }
    });
    return (_ctx, _push, _parent, _attrs) => {
      const _component_StatsCards = _sfc_main$3;
      const _component_TpsChart = _sfc_main$2;
      const _component_CumulativeChart = _sfc_main$1;
      _push(`<div${ssrRenderAttrs(mergeProps({ class: "min-h-screen p-3" }, _attrs))}><div class="flex items-center justify-between mb-3"><div class="flex items-center gap-3"><span class="text-sm text-gray-200 font-mono">nimbus/demo</span><span class="${ssrRenderClass([unref(sseConnected) ? "bg-green-500" : "bg-red-500", "inline-block w-2 h-2"])}"></span><span class="text-[11px] text-gray-600 font-mono">${ssrInterpolate(unref(sseConnected) ? "connected" : "disconnected")}</span>`);
      if (unref(lastUpdated)) {
        _push(`<span class="text-[11px] text-gray-700 font-mono"> last: ${ssrInterpolate(unref(lastUpdated))}</span>`);
      } else {
        _push(`<!---->`);
      }
      _push(`</div><div class="flex gap-px">`);
      if (!unref(generating)) {
        _push(`<button class="px-3 py-1.5 bg-green-900 hover:bg-green-800 text-green-300 text-xs font-mono transition-colors"> start </button>`);
      } else {
        _push(`<button class="px-3 py-1.5 bg-red-900 hover:bg-red-800 text-red-300 text-xs font-mono transition-colors"> stop </button>`);
      }
      _push(`</div></div>`);
      _push(ssrRenderComponent(_component_StatsCards, {
        "total-sent": unref(stats).total_sent,
        "total-confirmed": unref(stats).total_confirmed,
        "total-errors": unref(stats).total_errors,
        tps: unref(stats).tps,
        "uptime-secs": unref(stats).uptime_secs,
        "active-workers": unref(stats).active_workers
      }, null, _parent));
      _push(`<div class="grid grid-cols-2 gap-px mb-4">`);
      _push(ssrRenderComponent(_component_TpsChart, { data: unref(tpsHistory) }, null, _parent));
      _push(ssrRenderComponent(_component_CumulativeChart, { data: unref(cumulativeHistory) }, null, _parent));
      _push(`</div><div class="bg-black border border-[#222] p-2"><div class="text-[11px] text-gray-600 uppercase tracking-[0.05em] mb-1">events</div><div class="h-24 overflow-y-auto font-mono text-[11px] leading-[1.6]">`);
      if (unref(eventLog).length === 0) {
        _push(`<div class="text-gray-700">—</div>`);
      } else {
        _push(`<!---->`);
      }
      _push(`<!--[-->`);
      ssrRenderList(unref(eventLog).slice(0, 50), (line, i) => {
        _push(`<div class="text-gray-600">${ssrInterpolate(line)}</div>`);
      });
      _push(`<!--]--></div></div></div>`);
    };
  }
});
const _sfc_setup = _sfc_main.setup;
_sfc_main.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add("pages/index.vue");
  return _sfc_setup ? _sfc_setup(props, ctx) : void 0;
};
export {
  _sfc_main as default
};
//# sourceMappingURL=index-CrCCSiBU.js.map
