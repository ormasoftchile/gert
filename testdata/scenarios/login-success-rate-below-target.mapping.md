# Mapping Report — Login success rate is below target

## Mapping Table

| Step ID | TSG Heading Path | Source Lines | Type | Justification |
|---|---|---|---|---|
| obtain_database_details | ## Triage | L32-L48 | manual | Prose instructions and images to collect server/database details. |
| obtain_login_failure_cause | ## Triage | L50-L57 | manual | Visual inspection of Health Properties and incident title. |
| query_login_failures_kusto | ## Triage | L66-L84 | xts | Inline Kusto query provided to determine login failures. |
| check_application_scope | ## Triage | L96-L105 | manual | Conditional routing to other queues based on application type. |
| select_mitigation_tsg | ## Mitigation > ### Steps | L116-L158 | manual | Decision point selecting downstream TSG based on failure cause. |
| has_dumps | ## Mitigation > ### Steps | L120 | manual | Link-out to specific mitigation TSG. |
| has_high_latency_logins | ## Mitigation > ### Steps | L121 | manual | Link-out to specific mitigation TSG. |
| has_io_error | ## Mitigation > ### Steps | L123 | manual | Link-out to specific mitigation TSG. |
| login_errors_40613_84 | ## Mitigation > ### Steps | L150 | manual | Link-out to specific mitigation TSG. |

## Source Excerpts

### obtain_database_details
> Obtain the details of database, such as the Server Name and Database Name.  
> If a ticket doesn't contain LogicalServerName or DatabaseName values…

### obtain_login_failure_cause
> Obtain login failure cause. This information is present in the health property page and in the title of the incident.

### query_login_failures_kusto
> You can use a Kusto query, such as the one below, to determine login failures…

### check_application_scope
> The Gateway queue does not handle incidents for SQL MI or Open Source databases…

### select_mitigation_tsg
> Follow a respective TSG based on the login failure cause:

## Extracted Variables
- server_name (ICM custom field)
- database_name (ICM custom field)
- environment (ICM occurring location)
- login_failure_cause (prompt)

## Manual Step Reasons
- Visual inspection of incident metadata and Health Properties cannot be automated.
- Selection of downstream mitigation TSG depends on human interpretation of failure cause.

## TODOs / Uncertainties
- Mapping of all possible login failure causes assumes the incident title/Health Property provides a normalized value.
- Additional listed TSGs can be added as branches if new failure causes are identified.