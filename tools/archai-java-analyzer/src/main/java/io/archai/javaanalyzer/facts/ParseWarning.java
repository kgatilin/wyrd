package io.archai.javaanalyzer.facts;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonPropertyOrder;

/**
 * A non-fatal parse problem encountered while walking the source tree —
 * typically a single unparseable file in an otherwise valid root.
 *
 * <p>Hard failures (e.g. no parsable file at all) bypass JavaFacts and exit
 * the process with code 1.
 */
@JsonInclude(JsonInclude.Include.ALWAYS)
@JsonPropertyOrder({"file", "message"})
public final class ParseWarning {

    private String file = "";
    private String message = "";

    public ParseWarning() {}

    public ParseWarning(String file, String message) {
        this.file = file;
        this.message = message;
    }

    public String getFile() { return file; }
    public void setFile(String file) { this.file = file; }

    public String getMessage() { return message; }
    public void setMessage(String message) { this.message = message; }
}
