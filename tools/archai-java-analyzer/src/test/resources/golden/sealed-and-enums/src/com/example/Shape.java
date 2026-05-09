package com.example;

/**
 * Sealed interface — exercises permits + interface declaration.
 */
public sealed interface Shape permits Circle, Square {
    double area();
}
