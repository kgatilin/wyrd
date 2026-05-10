package com.example.repo;

import java.util.HashMap;
import java.util.Map;

public class Storage {
    private final Map<String, String> store = new HashMap<>();

    public void save(String key, String value) {
        store.put(key, value);
    }

    public String load(String key) {
        return store.get(key);
    }
}
