package com.sofarpc.cli.model;

import com.fasterxml.jackson.annotation.JsonIgnore;
import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

/**
 * Typed model for a single test case result.
 *
 * @author wuwh
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class CaseResult {

    @JsonProperty("case")
    private String caseName;
    private boolean passed;
    private Long latencyMs;
    private String error;

    // Internal only, not serialized to JSON
    @JsonIgnore
    private int exitCode;

    public CaseResult() {
    }

    public CaseResult(String caseName) {
        this.caseName = caseName;
    }

    public String getCaseName() {
        return caseName;
    }

    public void setCaseName(String caseName) {
        this.caseName = caseName;
    }

    public boolean isPassed() {
        return passed;
    }

    public void setPassed(boolean passed) {
        this.passed = passed;
    }

    public Long getLatencyMs() {
        return latencyMs;
    }

    public void setLatencyMs(Long latencyMs) {
        this.latencyMs = latencyMs;
    }

    public String getError() {
        return error;
    }

    public void setError(String error) {
        this.error = error;
    }

    public int getExitCode() {
        return exitCode;
    }

    public void setExitCode(int exitCode) {
        this.exitCode = exitCode;
    }

    @Override
    public String toString() {
        return "CaseResult{case='" + caseName + "', passed=" + passed + "}";
    }
}
