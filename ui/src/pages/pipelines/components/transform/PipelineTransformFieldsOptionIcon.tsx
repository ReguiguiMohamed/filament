import type { Icon as PhosphorIcon } from "@phosphor-icons/react";

import Icon, { IconVariant } from "@galaxy-io/dls/icons/Icon";
// @dls-migrate input-base.getInputIconSize: Removed: theme functions of the `withTheme` era; size and color come from the rung and tokens.
import { getInputIconSize, InputSize } from "@galaxy-io/dls/inputs/Input";

interface PipelineTransformFieldsOptionIconProps {
  icon: PhosphorIcon;
}

const PipelineTransformFieldsOptionIcon = ({ icon }: PipelineTransformFieldsOptionIconProps) => (
  <Icon
    component={icon}
    variant={IconVariant.SECONDARY}
    size={getInputIconSize(InputSize.MEDIUM)}
  />
);

export default PipelineTransformFieldsOptionIcon;
