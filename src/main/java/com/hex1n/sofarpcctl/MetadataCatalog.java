package com.hex1n.sofarpcctl;

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
    }

    public static class MethodMetadata {
        private String description;
        private String risk = "read";
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
