import { BarChart, LineChart } from "echarts/charts";
import {
  AriaComponent,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  MarkLineComponent,
  ToolboxComponent,
  TooltipComponent,
} from "echarts/components";
import * as echarts from "echarts/core";
import { CanvasRenderer } from "echarts/renderers";

echarts.use([
  BarChart,
  LineChart,
  AriaComponent,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  MarkLineComponent,
  ToolboxComponent,
  TooltipComponent,
  CanvasRenderer,
]);

/** echarts is the ECharts core with only the modules shed's charts use. */
export { echarts };

/**
 * quickIntro shortens a chart's first draw to a quick sweep and lets data
 * updates land without animating. Pass it as the chart's ref.
 */
export function quickIntro(chart: echarts.ECharts | null) {
  chart?.setOption({
    animationDuration: 300,
    animationEasing: "cubicOut",
    animationDurationUpdate: 0,
  });
}
