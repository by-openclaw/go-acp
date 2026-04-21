import { LengthOfNamesRequired } from '../../shared/length-of-names-required';
import { NameLength } from '../../shared/name-length';

/**
 * UpdateRenameRequest command enum
 *
 * Names Type Required
 *
 * @export
 * @enum {number}
 */
export enum NameType {
    /**
     * Name Type  (Byte = 00)
     * Type of Names Required : Source Name
     */
    SOURCE_NAME = 0x00,
    /**
     * Name Type  (Byte = 01)
     * Type of Names Required : Source Association Name
     */
    SOURCE_ASSOCIATION_NAME = 0x01,
    /**
     * Name Type  (Byte = 02)
     * Type of Names Required : Destination Association Name
     */
    DESTINATION_ASSOCIATION_NAME = 0x02,
    /**
     * Name Type  (Byte = 02)
     * Type of Names Required : UMD Label
     */
    UMD_LABEL = 0x03
}

/**
 * UpdateRenameRequest command options
 *
 * @export
 * @interface UpdateRenameRequestCommandOptions
 */
export interface UpdateRenameRequestCommandOptions {
    /**
     * Length of Names Required
     *
     * @type {NameLength}
     * @memberof UpdateRenameRequestCommandOptions
     */
    lengthOfNames: NameLength;

    /**
     * Name Type Required
     *
     * @type {NameType}
     * @memberof UpdateRenameRequestCommandOptions
     */
    nameOfType: NameType;
}

/**
 * Utility class providing some extra functionality on the UpdateRenameRequestCommandOptions command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command options
     *
     * @param {UpdateRenameRequestCommandOptions} options the command options
     * @returns {string} the command options textual representation
     * @memberof CommandOptionsUtility
     */
    static toString(options: UpdateRenameRequestCommandOptions): string {
        return (
            `Name Type:${this.toStringNameType(options.nameOfType)}, ` +
            `Name Length:${this.toStringLengthOfNamesRequired(options.lengthOfNames.type)}`
        );
    }

    /**
     * Gets a textual representation of the NameType
     *
     * @static
     * @param {NameType} data the NameType
     * @returns {string} the NameType textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringNameType(data: NameType): string {
        return `(${data} - ${NameType[data]})`;
    }

    /**
     * Gets a textual representation of the LengthOfNamesRequired
     *
     * @static
     * @param {LengthOfNamesRequired} data the LengthOfNamesRequired
     * @returns {string} the LengthOfNamesRequired textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringLengthOfNamesRequired(data: LengthOfNamesRequired): string {
        return `( ${data} - ${LengthOfNamesRequired[data]} )`;
    }

    /**
     * Gets boolean indicating whether the Source Name is applied
     * + General : false => not applied
     * + Source Name : true
     *
     * @static
     * @param {UpdateRenameRequestCommandOptions} options the command options
     * @returns {boolean} 'true' if the command is Source Name otherwise 'false'
     * @memberof CommandOptionsUtility
     */
    static isSourceName(options: UpdateRenameRequestCommandOptions): boolean {
        return options.nameOfType === NameType.SOURCE_NAME;
    }
}
