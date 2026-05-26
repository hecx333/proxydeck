import ReactEChartsCore from "echarts-for-react/lib/core";
import { PieChart, LineChart } from "echarts/charts";
import {
  GridComponent,
  LegendComponent,
  TooltipComponent,
  TitleComponent
} from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";
import * as echarts from "echarts/core";

echarts.use([
  PieChart,
  LineChart,
  GridComponent,
  LegendComponent,
  TooltipComponent,
  TitleComponent,
  CanvasRenderer
]);

export function Chart(props: React.ComponentProps<typeof ReactEChartsCore>) {
  return <ReactEChartsCore echarts={echarts} {...props} />;
}
