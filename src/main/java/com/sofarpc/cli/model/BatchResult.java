package com.sofarpc.cli.model;

import java.util.List;

/**
 * Typed model for batch test execution result.
 *
 * @author wuwh
 */
public class BatchResult {

    private int total;
    private int passed;
    private int failed;
    private String startTime;
    private String duration;
    private List<CaseResult> results;

    public BatchResult() {
    }

    public int getTotal() {
        return total;
    }

    public void setTotal(int total) {
        this.total = total;
    }

    public int getPassed() {
        return passed;
    }

    public void setPassed(int passed) {
        this.passed = passed;
    }

    public int getFailed() {
        return failed;
    }

    public void setFailed(int failed) {
        this.failed = failed;
    }

    public String getStartTime() {
        return startTime;
    }

    public void setStartTime(String startTime) {
        this.startTime = startTime;
    }

    public String getDuration() {
        return duration;
    }

    public void setDuration(String duration) {
        this.duration = duration;
    }

    public List<CaseResult> getResults() {
        return results;
    }

    public void setResults(List<CaseResult> results) {
        this.results = results;
    }

    @Override
    public String toString() {
        return "BatchResult{total=" + total + ", passed=" + passed + ", failed=" + failed + "}";
    }
}
