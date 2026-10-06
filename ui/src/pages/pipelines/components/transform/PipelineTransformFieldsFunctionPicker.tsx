import SelectInput, {
  SelectInputSize,
  SelectInputVariant,
} from "@galaxy-io/dls/inputs/SelectInput";

import type { TransformFunction } from "@/gen/ingestion/v1/transformations_pb";

import {
  TRANSFORM_SELECT_ERROR_MARK,
  TRANSFORM_SELECT_SEARCH_THRESHOLD,
} from "@/pages/pipelines/components/transform/constants";
import {
  getTransformFunctionChoices,
  isTransformExprComplete,
} from "@/pages/pipelines/components/transform/grammar/catalog";
import {
  usePipelineTransformFieldsEditor,
  usePipelineTransformFieldsEnvironment,
} from "@/pages/pipelines/components/transform/PipelineTransformFieldsProvider";
import {
  type TransformExpr,
  TransformExprKind,
} from "@/pages/pipelines/components/transform/types";
import {
  createTransformFunctionOption,
  filterTransformOptions,
} from "@/pages/pipelines/components/transform/utils";

interface PipelineTransformFieldsFunctionPickerProps {
  input: TransformExpr;
  inputType: string | undefined;
  fn: TransformFunction["name"];
  onChange: (fn: TransformFunction["name"]) => void;
  isError?: boolean;
}

const PipelineTransformFieldsFunctionPicker = ({
  input,
  inputType,
  fn,
  onChange,
  isError = false,
}: PipelineTransformFieldsFunctionPickerProps) => {
  const { isDisabled } = usePipelineTransformFieldsEditor();
  const { functionsByName } = usePipelineTransformFieldsEnvironment();
  const options = getTransformFunctionChoices(input, inputType, fn, functionsByName).map(
    createTransformFunctionOption,
  );
  const isInputPending =
    input.kind !== TransformExprKind.EMPTY && !isTransformExprComplete(input, functionsByName);

  return (
    <SelectInput
      options={options}
      /* @dls-migrate selectinput.value: `value` and `onChange` now carry option ids, not option objects. */ value={
        options.find((option) => option.id === fn) ?? null
      }
      onChange={(option) => onChange(option.id)}
      /* @dls-migrate selectinput.onReset: The clear button calls `onChange` with an empty value: move side effects there and add `isClearable`. */ onReset={() =>
        onChange("")
      }
      /* @dls-migrate selectinput.onSearch: Add `isSearchable`; the DLS filters, `onSearch` only receives the term. */ onSearch={
        options.length > TRANSFORM_SELECT_SEARCH_THRESHOLD ? filterTransformOptions : undefined
      }
      placeholder={isInputPending ? "Complete the input first" : "Choose a function"}
      variant={SelectInputVariant.TERTIARY}
      size={SelectInputSize.MEDIUM}
      error={isError ? TRANSFORM_SELECT_ERROR_MARK : undefined}
      isDisabled={isDisabled || isInputPending}
      fillWidth
    />
  );
};

export default PipelineTransformFieldsFunctionPicker;
