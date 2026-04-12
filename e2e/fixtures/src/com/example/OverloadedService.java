package com.example;

public interface OverloadedService {
    String ping(String value);

    String ping(String value, Integer times);

    String lookup(Long id);

    String lookup(String key);
}
