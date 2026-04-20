import { LengthOfNamesRequired } from './length-of-names-required';
/**
 * Gets Names properties
 *
 * @export
 * @class NameLength
 */
export class NameLength {
    /**
     * Name Length (Byte = 00)
     * Length of Names Required : 4-char names
     * numberOfNamesToFollow : 32 Names maximum per message
     * @static
     * @memberof NameLength
     */
    static readonly FOUR_CHAR_NAMES = new NameLength(LengthOfNamesRequired.FOUR_CHAR_NAMES, 4, 32);

    /**
     * Name Length (Byte = 01)
     * Length of Names Required : 8-char names
     * numberOfNamesToFollow : 16 Names maximum per message
     * @static
     * @memberof NameLength
     */
    static readonly EIGHT_CHAR_NAMES = new NameLength(LengthOfNamesRequired.EIGHT_CHAR_NAMES, 8, 16);

    /**
     * Name Length (Byte = 02)
     * Length of Names Required : 12-char names
     * numberOfNamesToFollow : 10 Names maximum per message
     * @static
     * @memberof NameLength
     */
    static readonly TWELVE_CHAR_NAMES = new NameLength(LengthOfNamesRequired.TWELVE_CHAR_NAMES, 12, 10);

    /**
     * Name Length (Byte = 03)
     * Length of Names Required : 16-char names
     * numberOfNamesToFollow : 8 Names maximum per message
     * @static
     * @memberof NameLength
     */
    static readonly SIXTEEN_CHAR_NAMES = new NameLength(LengthOfNamesRequired.SIXTEEN_CHAR_NAMES, 16, 8);

    /**
     *Creates an instance of NameLength.
     * @param {LengthOfNamesRequired} _type Length Of Names Required
     * @param {number} _byteLength Name Size
     * @param {number} _byteMaximumNumberOfNames maximum number of names per message
     * @memberof NameLength
     */
    protected constructor(
        private _type: LengthOfNamesRequired,
        private _byteLength: number,
        private _byteMaximumNumberOfNames: number
    ) {}

    /**
     * Gets the Length of names Required
     *
     * @readonly
     * @type {LengthOfNamesRequired}
     * @memberof NameLength
     */
    get type(): LengthOfNamesRequired {
        return this._type;
    }

    /**
     * Gets the name size
     *
     * @readonly
     * @type {number}
     * @memberof NameLength
     */
    get byteLength(): number {
        return this._byteLength;
    }

    /**
     * Gets the maximum number of names per message
     *
     * @readonly
     * @type {number}
     * @memberof NameLength
     */
    get byteMaximumNumberOfNames(): number {
        return this._byteMaximumNumberOfNames;
    }
}
