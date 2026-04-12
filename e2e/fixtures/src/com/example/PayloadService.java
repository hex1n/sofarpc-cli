package com.example;

import java.util.Map;

public interface PayloadService {
    Map<String, Object> submit(Map<String, Object> payload);
}
