package com.example;

/**
 * Enum with explicit constructor — exercises enum kind + enum_constants.
 */
public enum Color {
    RED("#ff0000"),
    GREEN("#00ff00"),
    BLUE("#0000ff");

    private final String hex;

    Color(String hex) {
        this.hex = hex;
    }

    public String hex() {
        return hex;
    }
}
