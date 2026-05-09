package com.example;

/**
 * Abstract base — exercises extends + abstract modifier.
 */
public abstract class Animal {
    protected final String name;

    protected Animal(String name) {
        this.name = name;
    }

    public abstract String sound();

    public String describe() {
        return name + " says " + sound();
    }
}
