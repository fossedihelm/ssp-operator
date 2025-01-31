# SSP Operator metrics
This document aims to help users that are not familiar with metrics exposed by the SSP Operator.
All metrics documented here are auto-generated by the utility tool `tools/metricsdocs` and reflects exactly what is being exposed.

## SSP Operator Metrics List
### kubevirt_ssp_num_of_operator_reconciling_properly
The total number of ssp-operator pods reconciling with no errors
### kubevirt_ssp_operator_up_total
The total number of running ssp-operator pods
### kubevirt_ssp_rejected_vms_total
The total number of vms rejected by virt-template-validator
### kubevirt_ssp_template_validator_up_total
The total number of running virt-template-validator pods
### kubevirt_ssp_total_restored_common_templates
The total number of common templates restored by the operator back to their original state
## Developing new metrics
After developing new metrics or changing old ones, please run `make generate-doc` to regenerate this document.
