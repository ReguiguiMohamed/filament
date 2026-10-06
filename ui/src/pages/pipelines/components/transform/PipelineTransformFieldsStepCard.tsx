import { match } from "ts-pattern";

import SelectInput, {
  SelectInputSize,
  SelectInputVariant,
  type SelectOption,
} from "@galaxy-io/dls/inputs/SelectInput";
import Flex, { AlignItems, FlexDirection } from "@galaxy-io/dls/layout/Flex";

import {
  TRANSFORM_BODY_INSET,
  TRANSFORM_HEADER_PADDING_X,
} from "@/pages/pipelines/components/transform/constants";
import { usePipelineTransformFieldsValidation } from "@/pages/pipelines/components/transform/hooks/usePipelineTransformFieldsValidation";
import {
  PipelineTransformFieldsEditorContext,
  usePipelineTransformFieldsActions,
  usePipelineTransformFieldsEnvironment,
} from "@/pages/pipelines/components/transform/PipelineTransformFieldsProvider";
import PipelineTransformFieldsRow, {
  PipelineTransformFieldsRowVariant,
} from "@/pages/pipelines/components/transform/PipelineTransformFieldsRow";
import PipelineTransformFieldsStepCompute from "@/pages/pipelines/components/transform/PipelineTransformFieldsStepCompute";
import PipelineTransformFieldsStepDrop from "@/pages/pipelines/components/transform/PipelineTransformFieldsStepDrop";
import PipelineTransformFieldsStepFooter from "@/pages/pipelines/components/transform/PipelineTransformFieldsStepFooter";
import PipelineTransformFieldsStepHeader from "@/pages/pipelines/components/transform/PipelineTransformFieldsStepHeader";
import PipelineTransformFieldsStepRename from "@/pages/pipelines/components/transform/PipelineTransformFieldsStepRename";
import PipelineTransformFieldsStepSurface from "@/pages/pipelines/components/transform/PipelineTransformFieldsStepSurface";
import PipelineTransformFieldsSubject from "@/pages/pipelines/components/transform/PipelineTransformFieldsSubject";
import {
  type PipelineTransformFieldsDraft,
  type PipelineTransformFieldsStepHandle,
  TransformStepKind,
} from "@/pages/pipelines/components/transform/types";

interface PipelineTransformFieldsStepCardProps {
  draft: PipelineTransformFieldsDraft;
  handle?: PipelineTransformFieldsStepHandle;
}

const PipelineTransformFieldsStepCard = ({
  draft,
  handle,
}: PipelineTransformFieldsStepCardProps) => {
  const { resources, isReadOnly } = usePipelineTransformFieldsEnvironment();
  const { setDraft, setResource, cancel, save, removeStep } = usePipelineTransformFieldsActions();
  const { editor, issues, warnings, typeSummary, isSaveDisabled } =
    usePipelineTransformFieldsValidation(draft);
  const resourceOptions: SelectOption[] = resources.map((resource) => ({
    id: resource,
    label: resource,
    value: resource,
  }));
  const { id, step } = draft;

  return (
    <PipelineTransformFieldsEditorContext.Provider value={editor}>
      <PipelineTransformFieldsStepSurface $isHoverable>
        <PipelineTransformFieldsStepHeader isOpen onToggle={cancel} handle={handle}>
          <PipelineTransformFieldsSubject step={step} outputIndex={0} onChange={setDraft} />
        </PipelineTransformFieldsStepHeader>
        <Flex
          alignItems={AlignItems.START}
          /* @dls-migrate layout.off-scale: Pick a value on the space scale (or a CSS-order tuple of them). */ padding={`0 ${TRANSFORM_HEADER_PADDING_X}px ${TRANSFORM_HEADER_PADDING_X}px ${TRANSFORM_BODY_INSET}px`}
          fillWidth
        >
          <Flex alignItems={AlignItems.START} direction={FlexDirection.COLUMN} gap={8} fillWidth>
            {id === null && resources.length > 1 && (
              <PipelineTransformFieldsRow
                variant={PipelineTransformFieldsRowVariant.STEP}
                gutter="for"
              >
                <SelectInput
                  options={resourceOptions}
                  /* @dls-migrate selectinput.value: `value` and `onChange` now carry option ids, not option objects. */ value={
                    resourceOptions.find((option) => option.id === draft.resource) ?? null
                  }
                  onChange={(option) => setResource(option.id)}
                  placeholder="Choose a resource"
                  variant={SelectInputVariant.TERTIARY}
                  size={SelectInputSize.MEDIUM}
                  isDisabled={isReadOnly}
                  fillWidth
                />
              </PipelineTransformFieldsRow>
            )}
            {match(step)
              .with({ kind: TransformStepKind.RENAME }, (rename) => (
                <PipelineTransformFieldsStepRename step={rename} onChange={setDraft} />
              ))
              .with({ kind: TransformStepKind.DROP }, (drop) => (
                <PipelineTransformFieldsStepDrop step={drop} onChange={setDraft} />
              ))
              .with({ kind: TransformStepKind.COMPUTE }, (compute) => (
                <PipelineTransformFieldsStepCompute step={compute} onChange={setDraft} />
              ))
              .exhaustive()}
            <PipelineTransformFieldsStepFooter
              issues={issues}
              warnings={warnings}
              typeSummary={typeSummary}
              isSaveDisabled={isSaveDisabled}
              isDisabled={isReadOnly}
              onSave={save}
              onCancel={cancel}
              onDelete={id === null ? undefined : () => removeStep(id)}
            />
          </Flex>
        </Flex>
      </PipelineTransformFieldsStepSurface>
    </PipelineTransformFieldsEditorContext.Provider>
  );
};

export default PipelineTransformFieldsStepCard;
