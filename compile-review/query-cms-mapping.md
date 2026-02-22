# Mapping Report – Query-CMS.md

## Step Mapping Table

| Step ID | TSG Section | Type | Justification |
|--------|-------------|------|---------------|
| run_cms_query_via_http_request_tool | Using the XTS HTTPRequest Query Tool | manual | Section is pure prose with UI-based instructions and no executable query text; per rules, this is emitted as a manual step with checklist evidence. |
| run_cms_query_via_adhoc_view | AdhocCMSQuery.xts View | xts (view) | TSG explicitly references an `.xts` view used to execute CMS queries; per rules, this is modeled as an XTS view step. |
| run_global_cms_query | Globally Querying All CMS Instances | xts (view) | TSG references the **Global Adhoc CMS Query.xts** view, which fans out to all CMS instances; per rules, this must be emitted as an XTS view step. |

## Extracted Variables

- None.  
  The TSG does not reference any parameterized values (such as server names, database names, or environment variables) that can be templated.

## Manual Step Reasons

- **run_cms_query_via_http_request_tool**:  
  The instructions are UI-driven and descriptive, with no concrete command, query text, or automation-safe artifact. According to the rules, pure prose sections are represented as manual steps.

## TODOs / Uncertainties

- No example T-SQL CMS queries are provided in the TSG. As a result:
  - The HttpRequest Query Tool step includes a TODO noting the lack of a concrete query.
  - The XTS view steps do not include query parameters or result captures.
- The XTS environment is not explicitly specified in the TSG; it is left as an empty default in `meta.xts.environment`.

## Notes

- The TSG serves as a **reference guide** rather than a linear incident response or mitigation procedure; therefore `meta.kind` is set to `reference`.
- No CLI steps or commands are present, so no governance `allowed_commands` section is emitted.