import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandOptionsUtility, MaintenanceMessageCommandOptions } from './options';
import { CommandParamsUtility, MaintenanceMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Maintenance Message command
 *
 * This message is issued by the remote device and allows various maintenance functions to be performed on the controller,
 * i.e. hard reset, soft reset, clear protects, configure installed modules, database transfer.
 * The number of bytes following the command byte is dependent on the operation being performed
 *
 * Command issued by the remote device
 *
 * @export
 * @class MaintenanceMessageCommand
 * @extends {CommandBase<MaintenanceMessageCommandParams>}
 */
export class MaintenanceMessageCommand extends CommandBase<
    MaintenanceMessageCommandParams,
    MaintenanceMessageCommandOptions
> {
    /**
     * Creates an instance of MaintenanceMessageCommand.
     *
     * @param {MaintenanceMessageCommandParams} params the command parameters
     * @param {MaintenanceMessageCommandOptions} options the command options
     * @memberof MaintenanceMessageCommand
     */
    constructor(params: MaintenanceMessageCommandParams, options: MaintenanceMessageCommandOptions) {
        super(CommandIdentifiers.RX.GENERAL.MAINTENANCE_MESSAGE, params, options);
        this.validateParams(params);
    }

    /**
     * Gets a textual representation of the command including params and options (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof CrossPointConnectMessageCommand
     */
    toLogDescription(): string {
        return CommandOptionsUtility.isClearProtects(this.options)
            ? `Clear Protects - ${this.name}: ${CommandOptionsUtility.toString(
                  this.options
              )}, ${CommandParamsUtility.toString(this.params)}`
            : `General - ${this.name}: ${CommandOptionsUtility.toString(this.options)}`;
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof CrossPointConnectMessageCommand
     */
    protected buildData(): Buffer {
        return CommandOptionsUtility.isClearProtects(this.options) ? this.buildDataExtended() : this.buildDataNormal();
    }

    /**
     * Validates the command parameters, options and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {MaintenanceMessageCommandParams} params the command parameters
     * @memberof CrossPointTallyMessageCommand
     */
    private validateParams(params: MaintenanceMessageCommandParams): void {
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
     * Build the general command
     * Returns Probel SW-P-08 - General Maintenance Message CMD_007_0X07
     *
     * + This message is issued by the remote device and allows various maintenance functions to be performed on the controller, i.e. hard reset, soft reset, clear protects, configure installed modules, database transfer.
     * + The number of bytes following the command byte is dependent on the operation being performed.
     *
     * | Message | Command Byte | 007 - 0x07                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  | Maintenance  | describe the functions and following message data.                                                                                 |
     * |         |   Function   |                                                                                                                                    |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof MaintenanceMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 2 })
            .writeUInt8(this.id)
            .writeUInt8(this.options.maintenanceFunction)
            .toBuffer();
    }

    /**
     * Builds the extended command
     * + Clear Protects selected Maintenance Message Function
     * Returns Probel SW-P-08 - General Maintenance Message CMD_007_0X07
     *
     * + This message is issued by the remote device and allows various maintenance functions to be performed on the controller, i.e. hard reset, soft reset, clear protects, configure installed modules, database transfer.
     * + The number of bytes following the command byte is dependent on the operation being performed.
     *
     * | Message | Command Byte | 007 - 0x07                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  | Maintenance  | describe the functions and following message data.                                                                                 |
     * |         |   Function   |                                                                                                                                    |
     * | Byte 2  | Matrix number| Matrix No (0-19) or  FFh = Clear all matrices                                                                                      |
     * | Byte 3  | Level number | Level No (0-15) or FFh = Clear all levels                                                                                          |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof MaintenanceMessageCommand
     */
    private buildDataExtended(): Buffer {
        return new SmartBuffer({ size: 4 })
            .writeUInt8(this.id)
            .writeUInt8(this.options.maintenanceFunction)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(this.params.levelId)
            .toBuffer();
    }
}
