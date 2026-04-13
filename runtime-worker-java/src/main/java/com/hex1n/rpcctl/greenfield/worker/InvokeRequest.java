package com.hex1n.rpcctl.greenfield.worker;

import com.fasterxml.jackson.databind.JsonNode;

import java.util.ArrayList;
import java.util.List;

public class InvokeRequest {
    public String requestId;
    public String service;
    public String method;
    public List<String> paramTypes = new ArrayList<String>();
    public JsonNode args;
    public String payloadMode = "raw";
    public TargetConfig target = new TargetConfig();
}
