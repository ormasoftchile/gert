# Login success rate is below target

Legacy TsgId: CRGW0001

## Manual Production Touch Safety
When using a CAS command that requires a PSA claim, refer to the [R2D certified CAS TSG](https://eng.ms/docs/cloud-ai-platform/azure-data/azure-data-azure-databases/sql-/sql-db-cross-queue-on-call-tsg/production-touch-safety/cas-with-psa#tsg-inventory---cas-with-psa) for production safety. If no such TSG is available, obtain EIM approval to address the incident. Once the incident is mitigated, notify bobpusateri to update the TSG.

## Background

This alert indicates that the login success rate of a Database is below the target. It gets evaluated every 5 minutes and is triggered if within the last 15 minutes:

1. The login success rate has been below 99%.
2. There have been more than 50 login attempts.
3. There have been login failures for 7 minutes or more (not necessarily consecutive).

The conditions are evaluated always on the context of individual databases, not aggregated per cluster/ring. Login failures might happen due to different causes. See [Triage](#triage) and [Mitigation](#mitigation) sections below for details.

> [!NOTE]
> Due to GDPR some elements in the alert message might be scrubbed.

Some mitigation steps may require restarting application instances. Restarting applications that are unavailable will typically not cause wider impact but restarting some shared applications, like Gateway or XDBHost, may cause transient impact for some customers.

**Extra caution must be taken when restarting several (5 or more) gateway instances.** Perform the mitigation in a slow rollout manner (apply a delay of 3 to 5 minutes before restarting multiple instances) and observe how the service responds to each application restart. Engage gateway experts in cases where you deem required.

## Triage

1. Obtain the details of database, such as the Server Name and Database Name. Those are typically present in the first incident discussion:

   ![Incident details](_media/login-success-rate-below-target/health-property-incident-details.png)

    If a ticket doesn't contain LogicalServerName or DatabaseName values:

    1. Open Associated Health Properties
        ![Health property link](_media/login-success-rate-below-target/health-property-link.png)
    2. On the Health Properties page, click on the top PropertyId.
    3. Then search through the Associated RCA rules for Kusto queries containing LogicalServerName and DatabaseName.

2. Obtain login failure cause. This information is present in the health property page and in the title of the incident:
   ![Login failure cause](_media/login-success-rate-below-target/login-failure-cause.png)

> [!NOTE]
> Health Properties may have expired. When a health property expires the DRI should still perform a basic impact assessment to confirm the issue is really mitigated or if impact is transient/intermittent.

This alert is created for login failures. If based on telemetry there are no current login failures and the Health Property is expired then mitigate the incident as transient.

You can use a Kusto query, such as the one below, to determine login failures or use the [Connectivity Livesite Troubleshooting](https://dataexplorer.azure.com/dashboards/ff00559d-35c6-41c1-93eb-43b67381a3cf?p-_startTime=1hours&p-_endTime=now&p-RegionFqdn=v-None&p-ClusterNameVar=v-None&p-NodeNameVar=v-None&p-SqlInstances=all&p-_logicalServerName=all&p-_databaseName=all#c8ef7d5e-a873-4aac-a5de-3c78971a1578) dashboard. In both cases scope the query/dashboard to the server and database from the incident.

(Replace variable placeholders for `ServerName` and `DatabaseName`)

```kql
let _ServerName = "<SERVER NAME>"; // Server Name, example: spartan-srv-oce-crmcor
let _DatabaseName = "<DATABASE NAME>"; // Database Name, example: db_crmcoreoce_202303
MonLogin
| where TIMESTAMP > ago(4h)
| where event == "process_login_finish"
| where logical_server_name == _ServerName or LogicalServerName == _ServerName
| where database_name == _DatabaseName and is_user_error == 0
| summarize Count = count() by bin(TIMESTAMP, 1m), strcat(iif(is_success, "Login Successful", "Login Failed"), "-", AppTypeName)
| render timechart
```

Be mindful that Kusto telemetry may be delayed by ~15 minutes. Therefore, check MDM-based telemetry to confirm if the issue is still occurring. You can use the `Login Rate + Login Success Rate` dashboard for this - it is MDM-based. A link to this dashboard is available on this page: [MDM Dashboards and Queries](../diagnostics-monitoring/mdm-dashboards-and-queries.md).

If based on telemetry there are no current login failures (only successes) and the Health Property is expired then mitigate the incident as transient. If login failures are still present or the Health Property is not expired then continue with next step.

> [!NOTE]
> The Gateway queue does not handle incidents for SQL MI or Open Source databases such as PostgreSQL or MySQL.
>
> If application is `fabric:/Host.MySQL/Host.MySQL`, transfer to the **Azure OSS Databases/Connectivity** queue.
>
> If application is `Gateway.PDC` or `Worker.CL`, transfer to the **SQL Managed Instance (Cloud Lifter)** queue, or go to [TSGCL0137: Massive login failures detected](https://eng.ms/docs/cloud-ai-platform/azure/azure-data/sql-mdcs-mi-polaris/sql-cloudlifter/sql-cloudlifter/sql_mi_livesite/tsgs/connectivity-and-networking/tsgcl0137-massive-login-failures-detected).

## Mitigation

### Pre-requisites

- Server and Database names (see [Triage](#triage))
- Login failure cause (see [Triage](#triage))

### Steps

From [Triage](#triage) you should have the database information, such as Database name and Server name and also the login failure cause.

Follow a respective TSG based on the login failure cause:

- [HasDumps](./availability-manager/has-dumps.md)
- [HasHighLatencyLoginsTopWaitStat](./availability-manager/has-high-latency-logins-top-wait-stat.md)
- [HasHighLatencyLoginsTopWaitStat_HADR_LOGPR](./availability-manager/has-high-latency-logins-top-wait-stat-hadr-logpr.md)
- [HasIoError](./availability-manager/has-io-errors.md)
- [HasTDEErrors](./availability-manager/has-tde-errors.md)
- [IsActivateDatabaseFailure](./availability-manager/is-activate-database-failure.md)
- [IsDW](./availability-manager/is-dw.md)
- [IsFedAuthLogin](./availability-manager/is-fed-auth-login.md)
- [IsFedAuthLogin - Error 33155](./login-errors/error-33155-is-fed-auth-login.md)
- [IsGatewayRoutingToWrongNode](./availability-manager/is-login-landing-on-wrong-node.md)
- [IsGWProxyBusy](./availability-manager/is-gw-proxy-busy.md)
- [IsGWProxyThrottledTCPTimeoutToBackend](./availability-manager/is-gw-proxy-throttled-tcp-timeout-to-backend.md)
- [IsLoginDispatcherHavingHighStalls](./availability-manager/is-login-dispatcher-having-high-stalls.md)
- [IsLoginHighCPUDueToKernelForSpecialSLOs](./availability-manager/is-login-high-cpu-due-to-kernel-for-special-slos.md)
- [IsLoginLandingOnWrongNode](./availability-manager/is-login-landing-on-wrong-node.md)
- [IsMasterBusyDueToBadLogins](./availability-manager/is-master-busy-due-to-bad-logins.md)
- [IsMonLoginDelaySuspected](./availability-manager/is-mon-login-delay-suspected.md)
- [IsNodeMissingTelemetry](./availability-manager/is-node-missing-telemetry.md)
- [IsPartitionInReconfigurationMostly](./availability-manager/is-partition-in-reconfiguration-mostly.md)
- [IsPeerCertificateInvalid](./availability-manager/is-peer-certificate-invalid.md)
- [IsRgManagerNotRunning](./availability-manager/is-rg-manager-not-running.md)
- [IsSessionLimit](./availability-manager/is-session-limit.md)
- [IsSniReadTimeout](./availability-manager/is-sni-read-timeout.md)
- [IsSniReadTimeoutDuringXdbhostDuplication](./availability-manager/is-sni-read-timeout-during-xdbhost-duplication.md)
- [IsSqlPartitionInErrorStateFromWinFabHealth](./availability-manager/is-sql-partition-in-error-state-from-win-fab-health.md)
- [IsSuspendDueTo9019](./availability-manager/is-suspend-due-to-9019.md)
- [IsTopWaitStatKnown](./availability-manager/is-top-wait-stat-known-x.md)
- [IsTopWaitStatKnown_LCK_M_S](./availability-manager/is-top-wait-stat-known-lck-m-s.md)
- [IsTopWaitStatKnown_LCK_M_SCH_M](./availability-manager/is-top-wait-stat-known-lck-m-sch-m.md)
- [IsTopWaitStatKnown_PREEMPTIVE_ODBCOPS](./availability-manager/is-top-wait-stat-known-preemptive-odbcops.md)
- [IsWinfabNamingServiceIssueOnTR](./availability-manager/is-win-fab-naming-service-issue-on-tr.md)
- [IsXdbhostUnhealthy](./availability-manager/is-xdbhost-unhealthy.md)
- [LoginDiffsFound](./availability-manager/login-diffs-found.md)
- [LoginDiffsFound: b-instance](./availability-manager/login-diffs-found-b-instance.md)
- [LookupErrorBackend_FABRIC_E_SERVICE_DOES_NOT_EXIST](../server-database/lookup-error-fabric-service-does-not-exist.md)
- [LookupErrorUnknown_FABRIC_E_GATEWAY_NOT_REACHABLE](./availability-manager/lookup-error-unknown-fabric-e-gateway-not-reachable.md)
- [LoginErrorsFound_40613_84](./login-errors/error-40613-state-84.md)

For other login errors such as `LoginErrorsFound` please see pages in [Login Errors](./login-errors/login-errors.md)

The following performance related TSGs describe an obsolete process dealing with incidents. As an initial step, try follow an available TSG. If further help needed, engage the experts.

- [IsHighCpu](./availability-manager/is-high-cpu.md)
- [IsOutOfMemory](./availability-manager/is-out-of-memory.md)
- [IsPotentialPerformanceRelatedHang](./availability-manager/is-potential-performance-related-hang.md)
- [IsPotentialPerformanceRelatedHang_Master](./availability-manager/is-potential-performance-related-hang-master.md)
- IsRegisterRGFailure - TODO

## Post-Mitigation
Post-mitigation steps should be located in the above-referenced TSG which included the mitigation steps.

## Escalation

If the above steps do not solve the issue, or you cannot confirm whether the issue has been mitigated, please escalate to `Azure SQL DB/Expert: Gateway escalations only from people in the Gateway queue`.

For low priority escalations use [sqldb_connectivity@microsoft.com](mailto:sqldb_connectivity@microsoft.com) or [Teams Channel](https://teams.microsoft.com/l/team/19%3a7f1808fac72347b3995b2159b606d6d8%40thread.skype/conversations?groupId=eda20e2c-41ad-4564-81d0-465b65c10f68&tenantId=72f988bf-86f1-41af-91ab-2d7cd011db47).

## References
- [The code repository for the availability alerts](https://msdata.visualstudio.com/Database%20Systems/_git/SqlTelemetry?path=%2FSrc%2FMdsRunners%2FMdsRunners%2FRunners%2FAvailability)
- [Transferring an Incident](https://icmdocs.azurewebsites.net/workflows/Incidents/Transferring%20an%20Incident.html)

## Ownership
Team: [Gateway SREs](mailto:gatewaysres@microsoft.com)  
Last Updated: @@LastModified