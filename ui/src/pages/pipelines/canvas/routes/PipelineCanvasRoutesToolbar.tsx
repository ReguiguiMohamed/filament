import { useMemo } from "react";

import { MagnifyingGlassIcon, PlusIcon } from "@phosphor-icons/react";
import pluralize from "pluralize";

import Button, { ButtonSize, ButtonVariant } from "@galaxy-io/dls/buttons/Button";
import { InputSize } from "@galaxy-io/dls/inputs/Input";
import MultiSelectInput, {
  MultiSelectInputSize,
  MultiSelectInputVariant,
} from "@galaxy-io/dls/inputs/MultiSelectInput";
import type { SelectOption } from "@galaxy-io/dls/inputs/SelectInput";
import TextInput from "@galaxy-io/dls/inputs/TextInput";
import Box from "@galaxy-io/dls/layout/Box";
import Flex, { AlignItems } from "@galaxy-io/dls/layout/Flex";
import FlexItem from "@galaxy-io/dls/layout/FlexItem";

import { ConnectorKind } from "@/gen/ingestion/v1/common_pb";

import ConnectorTile, { ConnectorTileSize } from "@/pages/connectors/components/ConnectorTile";
import { PIPELINE_CANVAS_VIEW_SWITCHER_INSET } from "@/pages/pipelines/canvas/constants";
import { usePipelineCanvasSelection } from "@/pages/pipelines/canvas/hooks/usePipelineCanvasSelection";
import PipelineCanvasViewSwitcher from "@/pages/pipelines/canvas/PipelineCanvasViewSwitcher";
import {
  PIPELINE_CANVAS_ROUTES_SEARCH_WIDTH,
  PIPELINE_CANVAS_ROUTES_SINK_FILTER_WIDTH,
  PIPELINE_CANVAS_ROUTES_SINKS_PINNED_OPTION_ID,
} from "@/pages/pipelines/canvas/routes/constants";
import { usePipelineCanvasRoutesSinks } from "@/pages/pipelines/canvas/routes/hooks/usePipelineCanvasRoutesSinks";
import type { CanvasNode } from "@/pages/pipelines/canvas/types";

interface PipelineCanvasRoutesToolbarProps {
  search: string;
  onSearchChange: (search: string) => void;
  canAddRoute: boolean;
  onAddRoute: () => void;
}

const PipelineCanvasRoutesToolbar = ({
  search,
  onSearchChange,
  canAddRoute,
  onAddRoute,
}: PipelineCanvasRoutesToolbarProps) => {
  const { sinkIds, setSinkIds } = usePipelineCanvasSelection();
  const sinks = usePipelineCanvasRoutesSinks();

  const sinkOptions = useMemo<SelectOption[]>(
    () =>
      sinks.map((sink) => ({
        id: sink.nodeId,
        label: sink.label,
        value: sink.nodeId,
        icon: (
          <ConnectorTile
            connector={sink.connection?.connector ?? ""}
            kind={ConnectorKind.SINK}
            size={ConnectorTileSize.SMALL}
            isDeleted={!!sink.connection?.deletedAt}
          />
        ),
      })),
    [sinks],
  );
  const selectedSinkOptions = sinkOptions.filter((option) => sinkIds.includes(option.id));
  const pinnedOptions = [
    {
      id: PIPELINE_CANVAS_ROUTES_SINKS_PINNED_OPTION_ID,
      label: "All sinks",
      optionIds: sinkOptions.map((option) => option.id),
    },
  ];

  const handleSinksChange = (selected: SelectOption[]) =>
    setSinkIds(
      selected.length === sinkOptions.length
        ? []
        : selected.map((option) => option.value as CanvasNode["id"]),
    );

  return (
    <Flex
      alignItems={AlignItems.CENTER}
      gap={12}
      /* @dls-migrate layout.off-scale: Pick a value on the space scale (or a CSS-order tuple of them). */ padding={`${PIPELINE_CANVAS_VIEW_SWITCHER_INSET}px`}
      shrink={0}
      fillWidth
    >
      <PipelineCanvasViewSwitcher />
      <Box width={PIPELINE_CANVAS_ROUTES_SEARCH_WIDTH}>
        <TextInput
          fillWidth
          value={search}
          onChange={onSearchChange}
          placeholder="Search resources"
          icon={MagnifyingGlassIcon}
          size={InputSize.MEDIUM}
        />
      </Box>
      <Box width={PIPELINE_CANVAS_ROUTES_SINK_FILTER_WIDTH}>
        <MultiSelectInput
          fillWidth
          options={sinkOptions}
          /* @dls-migrate multiselectinput.value: `value` and `onChange` now carry option ids, not option objects. */ value={
            selectedSinkOptions
          }
          onChange={handleSinksChange}
          placeholder="Sinks"
          variant={MultiSelectInputVariant.TERTIARY}
          size={MultiSelectInputSize.MEDIUM}
          /* @dls-migrate multiselectinput.pinnedOptions: Pinned rows are now option ids: pass `pinnedIds`. */ pinnedOptions={
            pinnedOptions
          }
          /* @dls-migrate multiselectinput.renderSelectedText: Merged into `renderValue(options)`. */ renderSelectedText={(
            selected,
            placeholder,
          ) => (selected.length ? pluralize("sink", selected.length, true) : placeholder)}
        />
      </Box>
      <FlexItem grow={1} />
      {canAddRoute && (
        <Button
          label="Add route"
          icon={PlusIcon}
          variant={ButtonVariant.SECONDARY}
          size={ButtonSize.MEDIUM}
          onClick={onAddRoute}
        />
      )}
    </Flex>
  );
};

export default PipelineCanvasRoutesToolbar;
