import { styled } from "@linaria/react";

import { t } from "@galaxy-io/dls/theme/tokens/t";

const PipelineTransformFieldsStepSurface = styled.div<{ $isHoverable: boolean }>`
  width: 100%;
  min-width: 0;

  transition: background-color 100ms ease;

  &:hover {
    background-color: ${({ $isHoverable }) =>
      $isHoverable ? t.color.background.secondary : "transparent"};
  }
`;

export default PipelineTransformFieldsStepSurface;
