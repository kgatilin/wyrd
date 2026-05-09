package com.example;

/**
 * Interface with a default method — exercises method modifiers and
 * interface-level dispatch.
 */
public interface Talker {

    String voice();

    default String shout(String msg) {
        return voice().toUpperCase() + ": " + msg;
    }
}
