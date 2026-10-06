import { XIcon } from "@phosphor-icons/react";

import Input, { InputSize, InputVariant } from "@galaxy-io/dls/inputs/Input";
import SelectInput, {
  SelectInputSize,
  SelectInputVariant,
} from "@galaxy-io/dls/inputs/SelectInput";
import TextInput from "@galaxy-io/dls/inputs/TextInput";
import { FontFamily } from "@galaxy-io/dls/theme/enums";

import {
  TRANSFORM_LITERAL_KIND_TO_ICON_MAP,
  TRANSFORM_SELECT_ERROR_MARK,
} from "@/pages/pipelines/components/transform/constants";
import { isTransformIntegerOnly } from "@/pages/pipelines/components/transform/grammar/catalog";
import { usePipelineTransformFieldsEditor } from "@/pages/pipelines/components/transform/PipelineTransformFieldsProvider";
import {
  type TransformLiteralExpr,
  TransformLiteralKind,
} from "@/pages/pipelines/components/transform/types";
import {
  createTransformBooleanOptions,
  getTransformNumberError,
} from "@/pages/pipelines/components/transform/utils";

const BOOLEAN_OPTIONS = createTransformBooleanOptions();

interface PipelineTransformFieldsLiteralProps {
  expr: TransformLiteralExpr;
  logicalTypes: string[];
  placeholder?: string;
  isError: boolean;
  onChange: (expr: TransformLiteralExpr) => void;
  onClear?: () => void;
}

const PipelineTransformFieldsLiteral = ({
  expr,
  logicalTypes,
  placeholder,
  isError,
  onChange,
  onClear,
}: PipelineTransformFieldsLiteralProps) => {
  const { isDisabled } = usePipelineTransformFieldsEditor();
  const trailing = onClear ? { icon: XIcon, onClick: onClear } : undefined;

  if (expr.literalKind === TransformLiteralKind.BOOLEAN) {
    return (
      <SelectInput
        options={BOOLEAN_OPTIONS}
        /* @dls-migrate selectinput.value: `value` and `onChange` now carry option ids, not option objects. */ value={
          BOOLEAN_OPTIONS.find((option) => option.id === expr.value) ?? null
        }
        onChange={(option) => onChange({ ...expr, value: option.id })}
        /* @dls-migrate selectinput.onReset: The clear button calls `onChange` with an empty value: move side effects there and add `isClearable`. */ onReset={
          onClear ?? (() => onChange({ ...expr, value: null }))
        }
        placeholder={placeholder}
        variant={SelectInputVariant.TERTIARY}
        size={SelectInputSize.MEDIUM}
        error={isError ? TRANSFORM_SELECT_ERROR_MARK : undefined}
        isDisabled={isDisabled}
        fillWidth
      />
    );
  }

  if (expr.literalKind === TransformLiteralKind.NUMBER) {
    const numberError =
      expr.value === null
        ? null
        : getTransformNumberError(expr.value, isTransformIntegerOnly(logicalTypes));
    return (
      <Input<string> /* @dls-migrate input-base.generic: Input is not generic: keep the value a string, or use `NumberInput`. */
        type="text"
        /* @dls-migrate input-base.parse: Removed: the value is the DOM string; `NumberInput` parses numbers. */ parse={(
          value,
        ) => value}
        value={expr.value ?? ""}
        onChange={(value) => onChange({ ...expr, value: value === "" ? null : value })}
        placeholder={placeholder}
        icon={TRANSFORM_LITERAL_KIND_TO_ICON_MAP[TransformLiteralKind.NUMBER]}
        trailing={trailing}
        isError={isError || numberError !== null}
        variant={InputVariant.TERTIARY}
        size={InputSize.MEDIUM}
        isDisabled={isDisabled}
        family={FontFamily.MONO}
        fillWidth
      />
    );
  }

  return (
    <TextInput
      value={expr.value ?? ""}
      onChange={(value) => onChange({ ...expr, value })}
      placeholder={placeholder}
      icon={TRANSFORM_LITERAL_KIND_TO_ICON_MAP[TransformLiteralKind.STRING]}
      trailing={trailing}
      isError={isError}
      variant={InputVariant.TERTIARY}
      size={InputSize.MEDIUM}
      isDisabled={isDisabled}
      fillWidth
    />
  );
};

export default PipelineTransformFieldsLiteral;
