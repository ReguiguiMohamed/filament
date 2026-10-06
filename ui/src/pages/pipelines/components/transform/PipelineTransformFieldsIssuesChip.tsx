import { WarningIcon } from "@phosphor-icons/react";

import Chip, { ChipSize, ChipVariant } from "@galaxy-io/dls/chips/Chip";
import Flex, { AlignItems, FlexDirection } from "@galaxy-io/dls/layout/Flex";
import Text, { TextSize } from "@galaxy-io/dls/text/Text";
import type { Placement } from "@galaxy-io/dls/theme/enums";
import Tooltip from "@galaxy-io/dls/tooltip/Tooltip";

const NO_WARNINGS: string[] = [];

interface PipelineTransformFieldsIssuesChipProps {
  issues: string[];
  warnings?: string[];
  position: Placement;
}

const PipelineTransformFieldsIssuesChip = ({
  issues,
  warnings = NO_WARNINGS,
  position,
}: PipelineTransformFieldsIssuesChipProps) => {
  const messages = issues.length > 0 ? issues : warnings;
  if (messages.length === 0) return null;
  return (
    <Tooltip
      placement={position}
      body={
        <Flex alignItems={AlignItems.START} direction={FlexDirection.COLUMN} gap={4}>
          {messages.map((message, index) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: the same message can repeat
            <Text key={index} size={TextSize.BODY_SM}>
              {message}
            </Text>
          ))}
        </Flex>
      }
    >
      <Chip
        label={issues.length > 0 ? "Invalid" : "Warning"}
        icon={WarningIcon}
        variant={issues.length > 0 ? ChipVariant.ERROR : ChipVariant.WARNING}
        size={ChipSize.SMALL}
      />
    </Tooltip>
  );
};

export default PipelineTransformFieldsIssuesChip;
