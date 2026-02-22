# Mapping Report — Alias DB Failure Alert

## Mapping Table

| Step ID | TSG Heading Path | Source Lines | Type | Justification |
|-------|-----------------|--------------|------|---------------|
| assess_gateway_alias_failures | ## Triage | L32-L48 | xts | Inline Kusto query provided to assess gateway failures; executed via XTS Kusto support. |
| assess_impacted_logins | ## Triage | L50-L66 | xts | Second inline Kusto query measuring impacted logins; suitable for XTS query mode. |
| check_alias_db_replica_health | ## Mitigation > ### 1. Check the health of the Alias database | L86-L90 | xts | Explicit instruction to open an XTS view (`SqlAliasCacheReplicas.xts`). |
| investigate_alias_db_health | ## Mitigation > ### 1. Check the health of the Alias database | L91-L99 | manual | Prose-only investigative steps requiring human judgment. |
| investigate_gateway_cert_dns_issues | ## Mitigation > ### 2. If the alias database is healthy | L103-L129 | manual | Diagnostic reasoning and checks (cert rollover, DNS) that cannot be safely automated. |
| restart_gateway_process_manual | ## Mitigation > ### 3. If none of the above | L133-L151 | manual | Contains destructive CAS command (Kill-Process), must remain manual per rules. |
| escalate_or_followup | ## Escalation | L153-L170 | manual | Escalation guidance and references, prose-only. |

## Source Excerpts

### assess_gateway_alias_failures
> “Run the below Kusto query to assess impact. Filter by the `ClusterName` reported in the incident.”  
> “MonRedirector | where originalEventTimestamp > ago(30m) | where event == "sql_alias_odbc_failure" …”

### assess_impacted_logins
> “Use this query to determine login connections impacted”  
> “MonLogin | where TIMESTAMP between (st .. et) … where error==40613 and state ==4”

### check_alias_db_replica_health
> “Open XTS and navigate to the view `SqlAliasCacheReplicas.xts`”

### investigate_alias_db_health
> “Once the database replicas corresponding to the alias db is loaded, check the replica health and that all replicas are healthy.”  
> “If any of the replicas are unhealthy… involve SQL DB Availability team”

### investigate_gateway_cert_dns_issues
> “Gateway uses certificate based authentication to connect to the Alias DB.”  
> “Name resolution issues in the ring… nslookup MN7 for example.”

### restart_gateway_process_manual
> “Kill the gateway process to mitigate”  
> “Do not restart more than two Gateways at the same time.”  
> “Get-FabricNode … | Kill-Process -ProcessName xdbgatewaymain.exe”

### escalate_or_followup
> “If you are on the Gateway Queue and need expert assistance please send a request assistance (RA)…”

## Extracted Variables

- **cluster_name**: Parsed from incident title.
- **environment**: From ICM occurring location instance.
- **start_time**: Incident impact start time.
- **end_time**: Prompted from engineer.
- **alias_app_name**: Prompted from engineer (identified via incident or XTS view).

## Manual Step Reasons

- Investigative steps require contextual judgment (replica health interpretation, cert/DNS analysis).
- Gateway restart uses a destructive CAS command (`Kill-Process`), prohibited from automation.

## TODOs / Uncertainties

- Confirm exact gateway NodeName before executing restart command.
- Confirm correct end time for impact analysis window.
- Validate Alias DB AppName when filtering MonSqlSystemHealth.