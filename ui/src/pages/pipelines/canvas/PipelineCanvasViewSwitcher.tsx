import ToggleInput, {
  ToggleInputSize,
  ToggleInputVariant,
  type ToggleOption,
} from "@galaxy-io/dls/inputs/ToggleInput";

import {
  PIPELINE_CANVAS_VIEW_TO_ICON_MAP,
  PIPELINE_CANVAS_VIEW_TO_LABEL_MAP,
} from "@/pages/pipelines/canvas/constants";
import { usePipelineCanvasSelection } from "@/pages/pipelines/canvas/hooks/usePipelineCanvasSelection";
import { PipelineCanvasView } from "@/pages/pipelines/canvas/types";

const PipelineCanvasViewSwitcher = () => {
  const { view, setView } = usePipelineCanvasSelection();

  const items: ToggleOption[] = Object.values(PipelineCanvasView).map((candidate) => ({
    id: candidate,
    label: PIPELINE_CANVAS_VIEW_TO_LABEL_MAP[candidate],
    icon: PIPELINE_CANVAS_VIEW_TO_ICON_MAP[candidate],
    onClick: () => setView(candidate),
  }));

  return (
    <ToggleInput
      options={items}
      value={view}
      size={ToggleInputSize.MEDIUM}
      variant={ToggleInputVariant.TERTIARY}
    />
  );
};

export default PipelineCanvasViewSwitcher;
