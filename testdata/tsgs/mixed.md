# Database Failover Guide

## 1. Verify Primary Status

Check the current primary database status:

```bash
az sql db show --name $DB_NAME --server $SQL_SERVER --resource-group $RESOURCE_GROUP
```

## 2. Review Connection Metrics

Log into the Azure portal and check the connection pool metrics. Look for spike in failed connections or timeouts.

## 3. Initiate Failover

If the primary is unhealthy, trigger a failover:

```bash
az sql db failover --name $DB_NAME --server $SQL_SERVER --resource-group $RESOURCE_GROUP
```

**WARNING**: This will cause a brief connection interruption. Ensure the application has retry logic.

## 4. Verify Failover Completion

Check that the new primary is healthy:

```bash
az sql db show --name $DB_NAME --server $SQL_SERVER --resource-group $RESOURCE_GROUP --query "status"
```

## 5. Notify Stakeholders

Send notification to the service owners about the failover event. Include the timestamp and reason.
