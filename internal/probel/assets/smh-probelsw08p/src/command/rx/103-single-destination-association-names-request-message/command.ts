import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandOptionsUtility, SingleDestinationAssociationNamesRequestMessageCommandOptions } from './options';
import { CommandParamsUtility, SingleDestinationAssociationNamesRequestMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Single Destination Association Names Request Message command
 *
 * This message is issued by the remote device to request the name for a single destination association for a given matrix.
 * The controller will respond with one DESTINATION ASSOCIATION NAMES RESPONSE message (Command Byte 107).
 *
 * Command issued by the remote device
 * @export
 * @class SingleDestinationAssociationNamesRequestMessageCommand
 * @extends {CommandBase<SingleDestinationAssociationNamesRequestMessageCommandParams>}
 */
export class SingleDestinationAssociationNamesRequestMessageCommand extends CommandBase<
    SingleDestinationAssociationNamesRequestMessageCommandParams,
    SingleDestinationAssociationNamesRequestMessageCommandOptions
> {
    /**
     * Creates an instance of SingleDestinationAssociationNamesRequestMessageCommand
     *
     * @param {SingleDestinationAssociationNamesRequestMessageCommandParams} params the command parameters
     * @param {LengthOfNamesRequiredCommandOptions} _options the command options
     * @memberof SingleDestinationAssociationNamesRequestMessageCommand
     */
    constructor(
        params: SingleDestinationAssociationNamesRequestMessageCommandParams,
        options: SingleDestinationAssociationNamesRequestMessageCommandOptions
    ) {
        super(SingleDestinationAssociationNamesRequestMessageCommand.getCommandId(params), params, options);
        this.validateParams(params);
    }

    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId and LevelId are 4 bits coded
     * + Extended : true
     *
     * @private
     * @static
     * @param {SingleDestinationAssociationNamesRequestMessageCommandParams} params the command parameters
     * @returns {boolean} 'true' is the command is extended otherwise 'false'
     * @memberof SingleDestinationAssociationNamesRequestMessageCommand
     */
    private static isExtended(params: SingleDestinationAssociationNamesRequestMessageCommandParams): boolean {
        // General Command is 4 bits = 16 range [0-15]
        if (params.matrixId > 15) {
            return true;
        }
        return false;
    }

    /**
     * Gets the Command Identifier based on the state of isExtended
     *
     * @private
     * @static
     * @param {SingleDestinationAssociationNamesRequestMessageCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof SingleDestinationAssociationNamesRequestMessageCommand
     */
    private static getCommandId(
        params: SingleDestinationAssociationNamesRequestMessageCommandParams
    ): CommandIdentifier {
        return SingleDestinationAssociationNamesRequestMessageCommand.isExtended(params)
            ? CommandIdentifiers.RX.EXTENDED.SINGLE_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE
            : CommandIdentifiers.RX.GENERAL.SINGLE_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof SingleDestinationAssociationNamesRequestMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}, ${CommandOptionsUtility.toString(
                this.options
            )}`;

        return SingleDestinationAssociationNamesRequestMessageCommand.isExtended(this.params)
            ? descriptionFor('Extended')
            : descriptionFor('General');
    }

    /**
     * Build the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof SingleDestinationAssociationNamesRequestMessageCommand
     */
    protected buildData(): Buffer {
        return SingleDestinationAssociationNamesRequestMessageCommand.isExtended(this.params)
            ? this.buildDataExtended()
            : this.buildDataNormal();
    }

    /**
     * Validates the command parameters, options and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {SingleDestinationAssociationNamesRequestMessageCommandParams} params the command parameters
     * @param {LengthOfNamesRequiredCommandOptions} options the command options
     * @memberof SingleDestinationAssociationNamesRequestMessageCommand
     */
    private validateParams(params: SingleDestinationAssociationNamesRequestMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = {
            ...new CommandParamsValidator(params).validate()
        };

        if (Object.keys(validationErrors).length > 0) {
            const message = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(CommandErrorsKeys.CREATE_COMMAND_FAILURE)
                .description;
            throw new ValidationError(message, validationErrors);
        }
    }

    /**
     * Builds the normal command
     * Returns Probel SW-P-08 - General Single Destination Association Names Request Message CMD_103_0X67
     *
     * + This message is issued by the remote device to request the name for a single destination association for a given matrix.
     * + The controller will respond with one DESTINATION ASSOCIATION NAMES RESPONSE message (Command Byte 107).
     *
     * | Message | Command Byte | 103 - 0x67                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |              | Matrix                                                                                                                             |
     * |         | Bits[4-7]    | Matrix Number                                                                                                                      |
     * |         | Bits[0-3]    | 0                                                                                                                                  |
     * | Byte 2  |Name Length   | Length of Names Required                                                                                                           |
     * |         | 0            | 4 char names                                                                                                                       |
     * |         | 1            | 8 char names                                                                                                                       |
     * |         | 2            | 12 char names                                                                                                                      |
     * | Byte 3  | Dest multiplier| Destination number DIV 256                                                                                                       |
     * | Byte 4  | Dest number  | Destination number MOD 256                                                                                                         |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof SingleDestinationAssociationNamesRequestMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 5 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, 0))
            .writeUInt8(this.options.lengthOfNames)
            .writeUInt8(Math.floor(this.params.destinationId / 256))
            .writeUInt8(this.params.destinationId % 256)
            .toBuffer();
    }

    /**
     * Builds the extended command
     * Returns Probel SW-P-08 - Extended General General Single Destination Association Names Request Message CMD_231_0Xe7
     *
     * + This message is issued by the remote device to request the name for a single destination association for a given matrix.
     * + The controller will respond with one EXTENDED DESTINATION ASSOCIATION NAMES RESPONSE message (Command Byte 235).
     *
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Message |  Command Byte   | 231 - 0xe7                                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format    | Notes                                                                                                                           |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte 1  | Matrix number   |                                                                                                                                 |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte 2  |Name Length      | Length of Names Required                                                                                                        |
     * |         | 0               | 4 char names                                                                                                                    |
     * |         | 1               | 8 char names                                                                                                                    |
     * |         | 2               | 12 char names                                                                                                                   |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte 3  | Dest multiplier | Destination number DIV 256                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte 4  | Dest number     | Destination number MOD 256                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof SingleDestinationAssociationNamesRequestMessageCommand
     */
    private buildDataExtended(): Buffer {
        return new SmartBuffer({ size: 5 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(this.options.lengthOfNames)
            .writeUInt8(Math.floor(this.params.destinationId / 256))
            .writeUInt8(this.params.destinationId % 256)
            .toBuffer();
    }
}
