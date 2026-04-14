package com.hex1n.sofarpc.worker;

import com.fasterxml.jackson.databind.JsonNode;

import java.util.ArrayList;
import java.util.List;

public class InvokeRequest {
    public String requestId;
    public String action;
    public String service;
    public String method;
    public List<String> paramTypes = new ArrayList<String>();
    public List<String> paramTypeSignatures = new ArrayList<String>();
    public JsonNode args;
    public String payloadMode = "raw";
    public boolean refresh;
    public TargetConfig target = new TargetConfig();
}
