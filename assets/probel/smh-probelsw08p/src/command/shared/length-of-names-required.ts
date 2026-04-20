/**
 * Gets the index of the length of Names Required
 * + 4 char names = 0
 * + 8 char names = 1
 * + 12 char names = 2
 * @export
 * @enum {number}
 */
export enum LengthOfNamesRequired {
    /**
     * Length of Names (Byte 2 = 00)
     * Length of Names Required : 4-char names
     */
    FOUR_CHAR_NAMES = 0x00,
    /**
     * Length of Names (Byte 2 = 01)
     * Length of Names Required : 8-char names
     */
    EIGHT_CHAR_NAMES = 0x01,
    /**
     * Length of Names (Byte 2 = 02)
     * Length of Names Required : 12-char names
     */
    TWELVE_CHAR_NAMES = 0x02,
    /**
     * Length of Names (Byte 2 = 03)
     * Length of Names Required : 16-char names
     */
    SIXTEEN_CHAR_NAMES = 0x03
}
