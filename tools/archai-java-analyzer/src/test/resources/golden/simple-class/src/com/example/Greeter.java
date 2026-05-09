package com.example;

import java.util.Objects;

/**
 * Simple greeter — single class, single field, single method.
 */
public class Greeter {
    private final String prefix;

    public Greeter(String prefix) {
        this.prefix = Objects.requireNonNull(prefix);
    }

    public String greet(String name) {
        return prefix + ", " + name + "!";
    }
}
