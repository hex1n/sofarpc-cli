package com.hex1n.sofarpcctl;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

final class DoctorReport {

    private final Map<String, Object> sections = new LinkedHashMap<String, Object>();
    private final List<Map<String, Object>> checks = new ArrayList<Map<String, Object>>();

    void putSection(String name, Object payload) {
        sections.put(name, payload);
    }

    void ok(String name, String message, Object details) {
        addCheck(name, "ok", message, details);
    }

    void warn(String name, String message, Object details) {
        addCheck(name, "warn", message, details);
    }

    void error(String name, String message, Object details) {
        addCheck(name, "error", message, details);
    }

    int exitCode() {
        return errorCount() == 0 ? ExitCodes.SUCCESS : ExitCodes.PARAMETER_ERROR;
    }

    Map<String, Object> toPayload() {
        Map<String, Object> payload = new LinkedHashMap<String, Object>(sections);
        payload.put("ok", errorCount() == 0);
        payload.put("warningCount", warningCount());
        payload.put("errorCount", errorCount());
        payload.put("checks", new ArrayList<Map<String, Object>>(checks));
        return payload;
    }

    private void addCheck(String name, String status, String message, Object details) {
        Map<String, Object> check = new LinkedHashMap<String, Object>();
        check.put("name", name);
        check.put("status", status);
        check.put("message", message);
        if (details != null) {
            check.put("details", details);
        }
        checks.add(check);
    }

    private int warningCount() {
        return countByStatus("warn");
    }

    private int errorCount() {
        return countByStatus("error");
    }

    private int countByStatus(String status) {
        int count = 0;
        for (Map<String, Object> check : checks) {
            if (status.equals(String.valueOf(check.get("status")))) {
                count++;
            }
        }
        return count;
    }
}
