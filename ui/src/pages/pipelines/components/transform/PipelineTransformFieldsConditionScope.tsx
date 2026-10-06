import SelectInput, {
  SelectInputSize,
  SelectInputVariant,
  type SelectOption,
} from "@galaxy-io/dls/inputs/SelectInput";

import { usePipelineTransformFieldsEditor } from "@/pages/pipelines/components/transform/PipelineTransformFieldsProvider";

const ALL_ROWS_ID = "all";
const MATCHING_ROWS_ID = "matching";

const SCOPE_OPTIONS: SelectOption[] = [
  { id: ALL_ROWS_ID, label: "All rows", value: ALL_ROWS_ID },
  { id: MATCHING_ROWS_ID, label: "Matching rows", value: MATCHING_ROWS_ID },
];

interface PipelineTransformFieldsConditionScopeProps {
  isMatching: boolean;
  onChange: (isMatching: boolean) => void;
}

const PipelineTransformFieldsConditionScope = ({
  isMatching,
  onChange,
}: PipelineTransformFieldsConditionScopeProps) => {
  const { isDisabled } = usePipelineTransformFieldsEditor();
  return (
    <SelectInput
      options={SCOPE_OPTIONS}
      /* @dls-migrate selectinput.value: `value` and `onChange` now carry option ids, not option objects. */ value={
        SCOPE_OPTIONS[isMatching ? 1 : 0]
      }
      onChange={(option) => onChange(option.id === MATCHING_ROWS_ID)}
      /* @dls-migrate selectinput.onReset: The clear button calls `onChange` with an empty value: move side effects there and add `isClearable`. */ onReset={() =>
        onChange(false)
      }
      variant={SelectInputVariant.TERTIARY}
      size={SelectInputSize.MEDIUM}
      isDisabled={isDisabled}
      fillWidth
    />
  );
};

export default PipelineTransformFieldsConditionScope;
