import { Fragment } from "react";

import { styled } from "@linaria/react";
import { match } from "ts-pattern";

import Chip, { ChipSize, ChipVariant } from "@galaxy-io/dls/chips/Chip";
import Span, { SpanVariant } from "@galaxy-io/dls/text/Span";
import { t } from "@galaxy-io/dls/theme/tokens/t";

import { TRANSFORM_ACTION } from "@/pages/pipelines/components/transform/constants";
import {
  formatTransformStep,
  TransformSummaryPartKind,
} from "@/pages/pipelines/components/transform/grammar/format";
import { usePipelineTransformFieldsEnvironment } from "@/pages/pipelines/components/transform/PipelineTransformFieldsProvider";
import type { TransformStep } from "@/pages/pipelines/components/transform/types";

const Sentence = styled.div`
  display: flex;
  align-items: center;
  min-height: ${TRANSFORM_ACTION}px;
  min-width: 0;

  font-family: ${t.font.sans.family};
  font-size: ${t.font.sans.size.body_md};
  line-height: ${TRANSFORM_ACTION}px;
  letter-spacing: ${t.font.sans.spacing.body_md};
  color: ${t.color.text.secondary};
  overflow-wrap: anywhere;
`;

const ChipSlot = styled.span`
  display: inline-flex;
  align-items: center;
  height: ${TRANSFORM_ACTION}px;
  padding: 0 2px;
  vertical-align: top;

  & span {
    font-family: ${t.font.mono.family};
    letter-spacing: ${t.font.mono.spacing.caption};
  }
`;

const SUMMARY_PART_KIND_TO_CHIP_VARIANT_MAP: Record<
  TransformSummaryPartKind.COLUMN | TransformSummaryPartKind.OUTPUT,
  ChipVariant
> = {
  [TransformSummaryPartKind.COLUMN]: ChipVariant.SECONDARY,
  [TransformSummaryPartKind.OUTPUT]: ChipVariant.PRIMARY,
};

interface PipelineTransformFieldsStepSummaryProps {
  step: TransformStep;
}

const PipelineTransformFieldsStepSummary = ({ step }: PipelineTransformFieldsStepSummaryProps) => {
  const { functionsByName } = usePipelineTransformFieldsEnvironment();
  return (
    <Sentence>
      <Span>
        {formatTransformStep(step, functionsByName).map((part, index) =>
          match(part.kind)
            .with(TransformSummaryPartKind.COLUMN, TransformSummaryPartKind.OUTPUT, (kind) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: parts are positional
              <ChipSlot key={index}>
                <Chip
                  label={part.text}
                  variant={SUMMARY_PART_KIND_TO_CHIP_VARIANT_MAP[kind]}
                  size={ChipSize.SMALL}
                />
              </ChipSlot>
            ))
            .with(TransformSummaryPartKind.VERB, () => (
              // biome-ignore lint/suspicious/noArrayIndexKey: parts are positional
              <Span key={index} variant={SpanVariant.PRIMARY}>
                {part.text}
              </Span>
            ))
            .with(TransformSummaryPartKind.TEXT, () => (
              // biome-ignore lint/suspicious/noArrayIndexKey: parts are positional
              <Fragment key={index}>{part.text}</Fragment>
            ))
            .exhaustive(),
        )}
      </Span>
    </Sentence>
  );
};

export default PipelineTransformFieldsStepSummary;
