import type { PropsWithChildren } from "react";

import { styled } from "@linaria/react";

import Widget, { WidgetVariant } from "@galaxy-io/dls/widget/Widget";

import {
  TRANSFORM_ACTION,
  TRANSFORM_BOX_PAD,
  TRANSFORM_GAP,
} from "@/pages/pipelines/components/transform/constants";

const Container = styled.div`
  container-type: inline-size;

  width: 100%;
  min-width: 0;
`;

const Rows = styled.div`
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr) ${TRANSFORM_ACTION}px;
  column-gap: ${TRANSFORM_GAP}px;
  row-gap: ${TRANSFORM_BOX_PAD}px;

  width: 100%;
  min-width: 0;

  & > * {
    grid-column: 1 / -1;
  }
`;

const PipelineTransformFieldsBox = ({ children }: PropsWithChildren) => (
  <Container>
    <Widget
      variant={
        WidgetVariant.TERTIARY
      } /* @dls-migrate widget.padding-other: The body inset is fixed at 12px: remove `padding` (use `isFlush` for 0). */
      padding={`${TRANSFORM_BOX_PAD}px`} /* @dls-migrate widget.fillWidth: Grow the card with a `FlexItem` or a `Grid` track. */
      fillWidth
    >
      <Rows>{children}</Rows>
    </Widget>
  </Container>
);

export default PipelineTransformFieldsBox;
