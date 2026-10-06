import { styled } from "@linaria/react";
import { FunctionIcon, WarningIcon } from "@phosphor-icons/react";
import pluralize from "pluralize";

import Icon, { IconVariant } from "@galaxy-io/dls/icons/Icon";
import Flex, { AlignItems, FlexDirection, JustifyContent } from "@galaxy-io/dls/layout/Flex";
import FlexItem from "@galaxy-io/dls/layout/FlexItem";
import Text, { TextSize, TextVariant, TextWeight } from "@galaxy-io/dls/text/Text";
import { t } from "@galaxy-io/dls/theme/tokens/t";

import {
  type PipelineCanvasValidationIssue,
  PipelineCanvasValidationIssueKind,
} from "@/pages/pipelines/canvas/hooks/usePipelineCanvasValidation";

const MAX_ISSUES_IN_TOOLTIP = 3;

const PipelineLayoutNavbarSaveIssueRow = styled.div<{ $isClickable: boolean }>`
  width: 100%;

  padding: 6px;

  display: flex;
  align-items: center;

  border: 0.5px solid ${t.color.border.primary};
  border-radius: 4px;

  background-color: ${t.color.background.primary};

  cursor: ${({ $isClickable }) => ($isClickable ? "pointer" : "default")};

  &:hover {
    background-color: ${({ $isClickable }) =>
      $isClickable ? t.color.background.secondary : t.color.background.primary};
  }
`;

interface PipelineLayoutNavbarSaveIssuesProps {
  issues: PipelineCanvasValidationIssue[];
  onSelectResource: (edgeId: string) => void;
}

// Lists what blocks Save: one row per invalid resource, then any graph-level
// problem, capped with a "+N more" line.
const PipelineLayoutNavbarSaveIssues = ({
  issues,
  onSelectResource,
}: PipelineLayoutNavbarSaveIssuesProps) => {
  const hidden = issues.length - MAX_ISSUES_IN_TOOLTIP;
  return (
    <Flex
      direction={FlexDirection.COLUMN}
      alignItems={AlignItems.CENTER}
      gap={4}
      minWidth="240px"
      maxWidth="320px"
    >
      {issues.slice(0, MAX_ISSUES_IN_TOOLTIP).map(({ edgeId, ...issue }) => (
        <PipelineLayoutNavbarSaveIssueRow
          key={`${edgeId ?? ""}|${issue.resource ?? ""}|${issue.message}`}
          $isClickable={edgeId !== undefined}
          onClick={edgeId === undefined ? undefined : () => onSelectResource(edgeId)}
        >
          <Flex
            alignItems={AlignItems.CENTER}
            justifyContent={JustifyContent.SPACE_BETWEEN}
            gap={16}
            fillWidth
          >
            <Flex gap={8} alignItems={AlignItems.CENTER} overflow="hidden">
              <Icon
                component={
                  issue.kind === PipelineCanvasValidationIssueKind.TRANSFORM
                    ? FunctionIcon
                    : WarningIcon
                }
                size={16}
                variant={IconVariant.SECONDARY}
              />
              <FlexItem shrink={1} minWidth={0} overflow="hidden">
                <Text size={TextSize.BODY_MD} lineClamp={1}>
                  {issue.kind === PipelineCanvasValidationIssueKind.TRANSFORM
                    ? issue.resource
                    : issue.message}
                </Text>
              </FlexItem>
            </Flex>
            {issue.invalidSteps !== undefined && (
              <Text size={TextSize.BODY_SM} weight={TextWeight.MEDIUM} variant={TextVariant.ERROR}>
                {issue.invalidSteps} {pluralize("issue", issue.invalidSteps)}
              </Text>
            )}
          </Flex>
        </PipelineLayoutNavbarSaveIssueRow>
      ))}
      {hidden > 0 && (
        <Flex
          alignItems={
            AlignItems.START
          } /* @dls-migrate layout.off-scale: Pick a value on the space scale (or a CSS-order tuple of them). */
          padding="6px 0"
        >
          <Text variant={TextVariant.TERTIARY} size={TextSize.BODY_SM}>
            +{hidden} more {pluralize("issue", hidden)}
          </Text>
        </Flex>
      )}
    </Flex>
  );
};

export default PipelineLayoutNavbarSaveIssues;
