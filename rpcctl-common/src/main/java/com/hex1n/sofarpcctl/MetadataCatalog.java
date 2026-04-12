package com.hex1n.sofarpcctl;

import com.fasterxml.jackson.annotation.JsonIgnore;
import com.fasterxml.jackson.annotation.JsonInclude;

import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

public class MetadataCatalog {

    private Map<String, ServiceMetadata> services = new LinkedHashMap<String, ServiceMetadata>();

    public Map<String, ServiceMetadata> getServices() {
        return services;
    }

    public void setServices(Map<String, ServiceMetadata> services) {
        this.services = services;
    }

    public ServiceMetadata getService(String serviceName) {
        if (services == null) {
            return null;
        }
        return services.get(serviceName);
    }

    public boolean isEmpty() {
        return services == null || services.isEmpty();
    }

    public static class ServiceMetadata {
        private String description;
        private String uniqueId;
        private Map<String, MethodMetadata> methods = new LinkedHashMap<String, MethodMetadata>();

        public String getDescription() {
            return description;
        }

        public void setDescription(String description) {
            this.description = description;
        }

        public String getUniqueId() {
            return uniqueId;
        }

        public void setUniqueId(String uniqueId) {
            this.uniqueId = uniqueId;
        }

        public Map<String, MethodMetadata> getMethods() {
            return methods;
        }

        public void setMethods(Map<String, MethodMetadata> methods) {
            this.methods = methods;
        }

        public MethodMetadata getMethod(String methodName) {
            if (methods == null) {
                return null;
            }
            return methods.get(methodName);
        }

        @JsonIgnore
        public int getOverloadCount() {
            if (methods == null || methods.isEmpty()) {
                return 0;
            }
            int count = 0;
            for (MethodMetadata methodMetadata : methods.values()) {
                count += methodMetadata.getResolvedOverloads().size();
            }
            return count;
        }
    }

    public static class MethodMetadata {
        private String description;
        private String risk = "read";
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        private List<String> paramTypes = Collections.emptyList();
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        private List<MethodOverload> overloads = Collections.emptyList();

        public String getDescription() {
            return description;
        }

        public void setDescription(String description) {
            this.description = description;
        }

        public String getRisk() {
            return risk;
        }

        public void setRisk(String risk) {
            this.risk = risk;
        }

        public List<String> getParamTypes() {
            return paramTypes;
        }

        public void setParamTypes(List<String> paramTypes) {
            this.paramTypes = paramTypes;
        }

        public List<MethodOverload> getOverloads() {
            return overloads;
        }

        public void setOverloads(List<MethodOverload> overloads) {
            this.overloads = overloads;
        }

        @JsonIgnore
        public List<MethodOverload> getResolvedOverloads() {
            if (overloads != null && !overloads.isEmpty()) {
                return overloads;
            }
            MethodOverload fallback = new MethodOverload();
            fallback.setDescription(description);
            fallback.setRisk(risk);
            fallback.setParamTypes(paramTypes == null ? Collections.<String>emptyList() : new ArrayList<String>(paramTypes));
            return Collections.singletonList(fallback);
        }

        @JsonIgnore
        public boolean hasOverloads() {
            return overloads != null && !overloads.isEmpty();
        }
    }

    public static class MethodOverload {
        private String description;
        private String risk = "read";
        @JsonInclude(JsonInclude.Include.NON_EMPTY)
        private List<String> paramTypes = Collections.emptyList();

        public String getDescription() {
            return description;
        }

        public void setDescription(String description) {
            this.description = description;
        }

        public String getRisk() {
            return risk;
        }

        public void setRisk(String risk) {
            this.risk = risk;
        }

        public List<String> getParamTypes() {
            return paramTypes;
        }

        public void setParamTypes(List<String> paramTypes) {
            this.paramTypes = paramTypes;
        }
    }
}
