import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { AllDestinationsAssociationNamesRequestMessageCommandOptions, CommandOptionsUtility } from './options';
import { AllDestinationsAssociationNamesRequestMessageCommandParams, CommandParamsUtility } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the All Destinations Association Names Request Message command
 *
 * This message is issued by the remote device to request the name for a single source association for a given matrix.
 * The controller will respond with one SOURCE ASSOCIATION NAMES RESPONSE message (Command Byte 116).
 *
 * command issued by the remote device
 * @export
 * @class AllDestinationsAssociationNamesRequestMessageCommand
 * @extends {CommandBase<AllDestinationsAssociationNamesRequestMessageCommandParams>}
 */
export class AllDestinationsAssociationNamesRequestMessageCommand extends CommandBase<
    AllDestinationsAssociationNamesRequestMessageCommandParams,
    AllDestinationsAssociationNamesRequestMessageCommandOptions
> {
    /**
     * Creates an instance of AllDestinationsAssociationNamesRequestMessageCommand
     *
     * @param {AllDestinationsAssociationNamesRequestMessageCommandParams} params the command parameters
     * @param {AllDestinationsAssociationNamesRequestMessageCommandOptions} _options the command options
     * @memberof AllDestinationsAssociationNamesRequestMessageCommand
     */
    constructor(
        params: AllDestinationsAssociationNamesRequestMessageCommandParams,
        options: AllDestinationsAssociationNamesRequestMessageCommandOptions
    ) {
        super(AllDestinationsAssociationNamesRequestMessageCommand.getCommandId(params), params, options);
        this.validateParams(params);
    }

    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false
     * + Extended : true
     *
     * @private
     * @static
     * @param {AllDestinationsAssociationNamesRequestMessageCommandParams} params the command parameters
     * @returns {boolean} 'true' if the command is extended otherwise 'false'
     * @memberof AllDestinationsAssociationNamesRequestMessageCommand
     */
    private static isExtended(params: AllDestinationsAssociationNamesRequestMessageCommandParams): boolean {
        // General Command is 4 bits = 16 range [0-15]
        return params.matrixId > 15;
    }

    /**
     * Gets the Command Identifier based on the state of isExtended
     *
     * @private
     * @static
     * @param {AllDestinationsAssociationNamesRequestMessageCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof AllDestinationsAssociationNamesRequestMessageCommand
     */
    private static getCommandId(params: AllDestinationsAssociationNamesRequestMessageCommandParams): CommandIdentifier {
        return AllDestinationsAssociationNamesRequestMessageCommand.isExtended(params)
            ? CommandIdentifiers.RX.EXTENDED.ALL_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE
            : CommandIdentifiers.RX.GENERAL.ALL_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof AllDestinationsAssociationNamesRequestMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}, ${CommandOptionsUtility.toString(
                this.options
            )}`;

        return AllDestinationsAssociationNamesRequestMessageCommand.isExtended(this.params)
            ? descriptionFor('Extended')
            : descriptionFor('General');
    }

    /**
     * Builds the command
     *
     * This message is issued by the remote device
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof AllDestinationsAssociationNamesRequestMessageCommand
     */
    protected buildData(): Buffer {
        return AllDestinationsAssociationNamesRequestMessageCommand.isExtended(this.params)
            ? this.buildDataExtended()
            : this.buildDataNormal();
    }

    /**
     * Validates the command parameters, options and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {AllDestinationsAssociationNamesRequestMessageCommandParams} params the command parameters
     * @memberof AllDestinationsAssociationNamesRequestMessageCommand
     */
    private validateParams(params: AllDestinationsAssociationNamesRequestMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = {
            ...new CommandParamsValidator(params).validate()
        };

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }

    /**
     * Builds the normal command
     * Returns Probel SW-P-08 - General All Destinations Association Names Request Message CMD_102_0X66
     *
     * + This message is issued by the remote device to request the names for all the destination associations for a given matrix.
     * + The controller will respond with one or more DESTINATION ASSOCIATION NAMES RESPONSE messages (Command Byte 107).
     *
     * | Message | Command Byte | 102 - 0x66                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |              | Matrix/Level number                                                                                                                |
     * |         | Bits[4-7]    | Matrix Number                                                                                                                      |
     * |         | Bits[0-3]    | 0                                                                                                                                  |
     * | Byte 2  |Name Length   | Length of Names Required                                                                                                           |
     * |         | 0            | 4 char names                                                                                                                       |
     * |         | 1            | 8 char names                                                                                                                       |
     * |         | 2            | 12 char names                                                                                                                      |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof AllDestinationsAssociationNamesRequestMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 3 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, 0))
            .writeUInt8(this.options.lengthOfNames)
            .toBuffer();
    }

    /**
     * Builds the extended command
     * Returns Probel SW-P-08 - Extended General General All Source Names Request Message CMD_230_0Xe6
     *
     * + This message is issued by the remote device to request the names for all the destination associations for a given matrix.
     * + The controller will respond with one or more EXTENDED DESTINATION ASSOCIATION NAMES RESPONSE messages (Command Byte 235).
     *
     * | Message |  Command Byte   | 230 - 0xe6                                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format    | Notes                                                                                                                           |
     * | Byte 1  | Matrix number   |                                                                                                                                 |
     * | Byte 2  | Name Length     | Length of Names Required                                                                                                        |
     * |         | 0               | 4 char names                                                                                                                    |
     * |         | 1               | 8 char names                                                                                                                    |
     * |         | 2               | 12 char names                                                                                                                   |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof AllDestinationsAssociationNamesRequestMessageCommand
     */
    private buildDataExtended(): Buffer {
        return new SmartBuffer({ size: 3 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(this.options.lengthOfNames)
            .toBuffer();
    }
}
